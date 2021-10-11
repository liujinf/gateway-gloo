package translator

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_config_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_type_matcher_v3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/wrappers"
	errors "github.com/rotisserie/eris"
	"github.com/solo-io/gloo/pkg/utils/regexutils"
	validationapi "github.com/solo-io/gloo/projects/gloo/pkg/api/grpc/validation"
	v1 "github.com/solo-io/gloo/projects/gloo/pkg/api/v1"
	"github.com/solo-io/gloo/projects/gloo/pkg/api/v1/core/matchers"
	v1plugins "github.com/solo-io/gloo/projects/gloo/pkg/api/v1/options"
	"github.com/solo-io/gloo/projects/gloo/pkg/plugins"
	"github.com/solo-io/gloo/projects/gloo/pkg/plugins/headers"
	"github.com/solo-io/gloo/projects/gloo/pkg/plugins/pluginutils"
	usconversion "github.com/solo-io/gloo/projects/gloo/pkg/upstreams"
	"github.com/solo-io/gloo/projects/gloo/pkg/utils"
	"github.com/solo-io/gloo/projects/gloo/pkg/utils/validation"
	"github.com/solo-io/go-utils/contextutils"
	"github.com/solo-io/solo-kit/pkg/api/v1/resources/core"
)

var (
	NoDestinationSpecifiedError = errors.New("must specify at least one weighted destination for multi destination routes")

	SubsetsMisconfiguredErr = errors.New("route has a subset config, but the upstream does not")
)

func (t *translatorInstance) computeRouteConfig(
	params plugins.Params,
	proxy *v1.Proxy,
	listener *v1.Listener,
	routeCfgName string,
	listenerReport *validationapi.ListenerReport,
) *envoy_config_route_v3.RouteConfiguration {
	if listener.GetHttpListener() == nil {
		return nil
	}

	httpListenerReport := listenerReport.GetHttpListenerReport()
	if httpListenerReport == nil {
		contextutils.LoggerFrom(params.Ctx).DPanic("internal error: listener report was not http type")
	}

	params.Ctx = contextutils.WithLogger(params.Ctx, "compute_route_config."+routeCfgName)

	virtualHosts := t.computeVirtualHosts(params, proxy, listener, httpListenerReport)

	// validate ssl config if the listener specifies any
	if err := validateListenerSslConfig(params, listener); err != nil {
		validation.AppendListenerError(listenerReport,
			validationapi.ListenerReport_Error_SSLConfigError,
			err.Error(),
		)
	}

	return &envoy_config_route_v3.RouteConfiguration{
		Name:                           routeCfgName,
		VirtualHosts:                   virtualHosts,
		MaxDirectResponseBodySizeBytes: listener.GetRouteOptions().GetMaxDirectResponseBodySizeBytes(),
	}
}

func (t *translatorInstance) computeVirtualHosts(
	params plugins.Params,
	proxy *v1.Proxy,
	listener *v1.Listener,
	httpListenerReport *validationapi.HttpListenerReport,
) []*envoy_config_route_v3.VirtualHost {
	httpListener, ok := listener.GetListenerType().(*v1.Listener_HttpListener)
	if !ok {
		return nil
	}
	virtualHosts := httpListener.HttpListener.GetVirtualHosts()
	ValidateVirtualHostDomains(virtualHosts, httpListenerReport)
	requireTls := len(listener.GetSslConfigurations()) > 0
	var envoyVirtualHosts []*envoy_config_route_v3.VirtualHost
	for i, virtualHost := range virtualHosts {
		vhostParams := plugins.VirtualHostParams{
			Params:   params,
			Listener: listener,
			Proxy:    proxy,
		}
		vhostReport := httpListenerReport.GetVirtualHostReports()[i]
		envoyVirtualHosts = append(envoyVirtualHosts, t.computeVirtualHost(vhostParams, virtualHost, requireTls, vhostReport))
	}
	return envoyVirtualHosts
}

func (t *translatorInstance) computeVirtualHost(
	params plugins.VirtualHostParams,
	virtualHost *v1.VirtualHost,
	requireTls bool,
	vhostReport *validationapi.VirtualHostReport,
) *envoy_config_route_v3.VirtualHost {

	// Make copy to avoid modifying the snapshot
	virtualHost = proto.Clone(virtualHost).(*v1.VirtualHost)
	virtualHost.Name = utils.SanitizeForEnvoy(params.Ctx, virtualHost.GetName(), "virtual host")

	var envoyRoutes []*envoy_config_route_v3.Route
	for i, route := range virtualHost.GetRoutes() {
		routeParams := plugins.RouteParams{
			VirtualHostParams: params,
			VirtualHost:       virtualHost,
		}
		routeReport := vhostReport.GetRouteReports()[i]
		generatedName := fmt.Sprintf("%s-route-%d", virtualHost.GetName(), i)
		computedRoutes := t.envoyRoutes(routeParams, routeReport, route, generatedName)
		envoyRoutes = append(envoyRoutes, computedRoutes...)
	}
	domains := virtualHost.GetDomains()
	if len(domains) == 0 || (len(domains) == 1 && domains[0] == "") {
		domains = []string{"*"}
	}
	var envoyRequireTls envoy_config_route_v3.VirtualHost_TlsRequirementType
	if requireTls {
		// TODO (ilackarms): support external-only TLS
		envoyRequireTls = envoy_config_route_v3.VirtualHost_ALL
	}

	out := &envoy_config_route_v3.VirtualHost{
		Name:       virtualHost.GetName(),
		Domains:    domains,
		Routes:     envoyRoutes,
		RequireTls: envoyRequireTls,
	}

	// run the plugins
	for _, plug := range t.plugins {
		virtualHostPlugin, ok := plug.(plugins.VirtualHostPlugin)
		if !ok {
			continue
		}
		if err := virtualHostPlugin.ProcessVirtualHost(params, virtualHost, out); err != nil {
			validation.AppendVirtualHostError(
				vhostReport,
				validationapi.VirtualHostReport_Error_ProcessingError,
				fmt.Sprintf("invalid virtual host [%s]: %v", virtualHost.GetName(), err.Error()),
			)
		}
	}
	return out
}

func (t *translatorInstance) envoyRoutes(
	params plugins.RouteParams,
	routeReport *validationapi.RouteReport,
	in *v1.Route,
	generatedName string,
) []*envoy_config_route_v3.Route {

	out := initRoutes(params, in, routeReport, generatedName)

	for i := range out {
		t.setAction(params, routeReport, in, out[i])
	}

	return out
}

// creates Envoy routes for each matcher provided on our Gateway route
func initRoutes(
	params plugins.RouteParams,
	in *v1.Route,
	routeReport *validationapi.RouteReport,
	generatedName string,
) []*envoy_config_route_v3.Route {

	if len(in.GetMatchers()) == 0 {
		return []*envoy_config_route_v3.Route{
			{
				Match: &envoy_config_route_v3.RouteMatch{
					PathSpecifier: &envoy_config_route_v3.RouteMatch_Prefix{Prefix: "/"},
				},
			},
		}
	}

	out := make([]*envoy_config_route_v3.Route, len(in.GetMatchers()))
	for i, matcher := range in.GetMatchers() {
		if matcher.GetPathSpecifier() == nil {
			validation.AppendRouteError(routeReport,
				validationapi.RouteReport_Error_InvalidMatcherError,
				"no path specifier provided",
				generatedName,
			)
		}
		match := GlooMatcherToEnvoyMatcher(params.Params.Ctx, matcher)
		out[i] = &envoy_config_route_v3.Route{
			Match: &match,
		}
		if in.GetName() != "" {
			out[i].Name = fmt.Sprintf("%s-%s-matcher-%d", generatedName, in.GetName(), i)
		} else {
			out[i].Name = fmt.Sprintf("%s-matcher-%d", generatedName, i)
		}
	}

	return out
}

// utility function to transform gloo matcher to envoy route matcher
func GlooMatcherToEnvoyMatcher(ctx context.Context, matcher *matchers.Matcher) envoy_config_route_v3.RouteMatch {
	match := envoy_config_route_v3.RouteMatch{
		Headers:         envoyHeaderMatcher(ctx, matcher.GetHeaders()),
		QueryParameters: envoyQueryMatcher(ctx, matcher.GetQueryParameters()),
	}
	if len(matcher.GetMethods()) > 0 {
		match.Headers = append(match.GetHeaders(), &envoy_config_route_v3.HeaderMatcher{
			Name: ":method",
			HeaderMatchSpecifier: &envoy_config_route_v3.HeaderMatcher_SafeRegexMatch{
				SafeRegexMatch: regexutils.NewRegex(ctx, strings.Join(matcher.GetMethods(), "|")),
			},
		})
	}
	// need to do this because Go's proto implementation makes oneofs private
	// which genius thought of that?
	setEnvoyPathMatcher(ctx, matcher, &match)
	match.CaseSensitive = matcher.GetCaseSensitive()
	return match
}

func (t *translatorInstance) setAction(
	params plugins.RouteParams,
	routeReport *validationapi.RouteReport,
	in *v1.Route,
	out *envoy_config_route_v3.Route,
) {
	switch action := in.GetAction().(type) {
	case *v1.Route_RouteAction:
		if err := ValidateRouteDestinations(params.Snapshot, action.RouteAction); err != nil {
			validation.AppendRouteWarning(routeReport,
				validationapi.RouteReport_Warning_InvalidDestinationWarning,
				err.Error(),
			)
		}

		out.Action = &envoy_config_route_v3.Route_Route{
			Route: &envoy_config_route_v3.RouteAction{},
		}
		if err := t.setRouteAction(params, action.RouteAction, out.GetAction().(*envoy_config_route_v3.Route_Route).Route, routeReport, out.GetName()); err != nil {
			if isWarningErr(err) {
				validation.AppendRouteWarning(routeReport,
					validationapi.RouteReport_Warning_InvalidDestinationWarning,
					err.Error(),
				)
			} else {
				validation.AppendRouteError(routeReport,
					validationapi.RouteReport_Error_ProcessingError,
					err.Error(),
					out.GetName(),
				)
			}
		}

		// run the plugins for RoutePlugin
		for _, plug := range t.plugins {
			routePlugin, ok := plug.(plugins.RoutePlugin)
			if !ok {
				continue
			}
			if err := routePlugin.ProcessRoute(params, in, out); err != nil {
				// plugins can return errors on missing upstream/upstream group
				// we only want to report errors that are plugin-specific
				// missing upstream(group) should produce a warning above
				if isWarningErr(err) {
					continue
				}
				validation.AppendRouteError(routeReport,
					validationapi.RouteReport_Error_ProcessingError,
					fmt.Sprintf("%T: %v", routePlugin, err.Error()),
					out.GetName(),
				)
			}
		}

		// run the plugins for RouteActionPlugin
		for _, plug := range t.plugins {
			routeActionPlugin, ok := plug.(plugins.RouteActionPlugin)
			if !ok || in.GetRouteAction() == nil || out.GetRoute() == nil {
				continue
			}
			raParams := plugins.RouteActionParams{
				RouteParams: params,
				Route:       in,
			}
			if err := routeActionPlugin.ProcessRouteAction(raParams, in.GetRouteAction(), out.GetRoute()); err != nil {
				// same as above
				if isWarningErr(err) {
					continue
				}
				validation.AppendRouteError(routeReport,
					validationapi.RouteReport_Error_ProcessingError,
					err.Error(),
					out.GetName(),
				)
			}
		}

	case *v1.Route_DirectResponseAction:
		out.Action = &envoy_config_route_v3.Route_DirectResponse{
			DirectResponse: &envoy_config_route_v3.DirectResponseAction{
				Status: action.DirectResponseAction.GetStatus(),
				Body:   DataSourceFromString(action.DirectResponseAction.GetBody()),
			},
		}

		// DirectResponseAction supports header manipulation, so we want to process the corresponding plugin.
		// See here: https://www.envoyproxy.io/docs/envoy/latest/api-v2/api/v2/route/route.proto#route-directresponseaction
		for _, plug := range t.plugins {
			routePlugin, ok := plug.(*headers.Plugin)
			if !ok {
				continue
			}
			if err := routePlugin.ProcessRoute(params, in, out); err != nil {
				if isWarningErr(err) {
					continue
				}
				validation.AppendRouteError(routeReport,
					validationapi.RouteReport_Error_ProcessingError,
					fmt.Sprintf("%T: %v", routePlugin, err.Error()),
					out.GetName(),
				)
			}
		}

	case *v1.Route_RedirectAction:
		out.Action = &envoy_config_route_v3.Route_Redirect{
			Redirect: &envoy_config_route_v3.RedirectAction{
				HostRedirect:           action.RedirectAction.GetHostRedirect(),
				ResponseCode:           envoy_config_route_v3.RedirectAction_RedirectResponseCode(action.RedirectAction.GetResponseCode()),
				SchemeRewriteSpecifier: &envoy_config_route_v3.RedirectAction_HttpsRedirect{HttpsRedirect: action.RedirectAction.GetHttpsRedirect()},
				StripQuery:             action.RedirectAction.GetStripQuery(),
			},
		}

		switch pathRewrite := action.RedirectAction.GetPathRewriteSpecifier().(type) {
		case *v1.RedirectAction_PathRedirect:
			out.GetAction().(*envoy_config_route_v3.Route_Redirect).Redirect.PathRewriteSpecifier = &envoy_config_route_v3.RedirectAction_PathRedirect{
				PathRedirect: pathRewrite.PathRedirect,
			}
		case *v1.RedirectAction_PrefixRewrite:
			out.GetAction().(*envoy_config_route_v3.Route_Redirect).Redirect.PathRewriteSpecifier = &envoy_config_route_v3.RedirectAction_PrefixRewrite{
				PrefixRewrite: pathRewrite.PrefixRewrite,
			}
		}
	}
}

func (t *translatorInstance) setRouteAction(params plugins.RouteParams, in *v1.RouteAction, out *envoy_config_route_v3.RouteAction, routeReport *validationapi.RouteReport, routeName string) error {
	switch dest := in.GetDestination().(type) {
	case *v1.RouteAction_Single:
		out.ClusterSpecifier = &envoy_config_route_v3.RouteAction_Cluster{}
		usRef, err := usconversion.DestinationToUpstreamRef(dest.Single)
		if err != nil {
			return err
		}
		out.GetClusterSpecifier().(*envoy_config_route_v3.RouteAction_Cluster).Cluster = UpstreamToClusterName(usRef)

		out.MetadataMatch = getSubsetMatch(dest.Single)

		return checkThatSubsetMatchesUpstream(params.Params, dest.Single)
	case *v1.RouteAction_Multi:
		return t.setWeightedClusters(params, dest.Multi, out, routeReport, routeName)
	case *v1.RouteAction_UpstreamGroup:
		upstreamGroupRef := dest.UpstreamGroup
		upstreamGroup, err := params.Snapshot.UpstreamGroups.Find(upstreamGroupRef.GetNamespace(), upstreamGroupRef.GetName())
		if err != nil {
			// the UpstreamGroup isn't found but set a bogus cluster so route replacement will still work
			out.ClusterSpecifier = &envoy_config_route_v3.RouteAction_Cluster{
				Cluster: "",
			}
			return pluginutils.NewUpstreamGroupNotFoundErr(*upstreamGroupRef)
		}
		md := &v1.MultiDestination{
			Destinations: upstreamGroup.GetDestinations(),
		}
		return t.setWeightedClusters(params, md, out, routeReport, routeName)
	case *v1.RouteAction_ClusterHeader:
		// ClusterHeader must use the naming convention {{namespace}}_{{clustername}}
		out.ClusterSpecifier = &envoy_config_route_v3.RouteAction_ClusterHeader{
			ClusterHeader: in.GetClusterHeader(),
		}
		return nil
	}
	return errors.Errorf("unknown upstream destination type")
}

func (t *translatorInstance) setWeightedClusters(params plugins.RouteParams, multiDest *v1.MultiDestination, out *envoy_config_route_v3.RouteAction, routeReport *validationapi.RouteReport, routeName string) error {
	clusterSpecifier := &envoy_config_route_v3.RouteAction_WeightedClusters{
		WeightedClusters: &envoy_config_route_v3.WeightedCluster{},
	}
	out.ClusterSpecifier = clusterSpecifier

	if len(multiDest.GetDestinations()) == 0 {
		return NoDestinationSpecifiedError
	}

	var totalWeight uint32
	for _, weightedDest := range multiDest.GetDestinations() {

		usRef, err := usconversion.DestinationToUpstreamRef(weightedDest.GetDestination())
		if err != nil {
			return err
		}

		totalWeight += weightedDest.GetWeight()

		weightedCluster := &envoy_config_route_v3.WeightedCluster_ClusterWeight{
			Name:          UpstreamToClusterName(usRef),
			Weight:        &wrappers.UInt32Value{Value: weightedDest.GetWeight()},
			MetadataMatch: getSubsetMatch(weightedDest.GetDestination()),
		}

		// run the plugins for Weighted Destinations
		for _, plug := range t.plugins {
			weightedDestinationPlugin, ok := plug.(plugins.WeightedDestinationPlugin)
			if !ok {
				continue
			}
			if err := weightedDestinationPlugin.ProcessWeightedDestination(params, weightedDest, weightedCluster); err != nil {
				validation.AppendRouteError(routeReport,
					validationapi.RouteReport_Error_ProcessingError,
					err.Error(),
					routeName,
				)
			}
		}

		clusterSpecifier.WeightedClusters.Clusters = append(clusterSpecifier.WeightedClusters.GetClusters(), weightedCluster)

		if err = checkThatSubsetMatchesUpstream(params.Params, weightedDest.GetDestination()); err != nil {
			return err
		}
	}

	clusterSpecifier.WeightedClusters.TotalWeight = &wrappers.UInt32Value{Value: totalWeight}

	return nil
}

// TODO(marco): when we update the routing API we should move this to a RouteActionPlugin
func getSubsetMatch(destination *v1.Destination) *envoy_config_core_v3.Metadata {
	var routeMetadata *envoy_config_core_v3.Metadata

	// TODO(yuval-k): should we add validation that the route subset indeed exists in the upstream?
	// First convert the subset information on the base destination, if present
	if destination.GetSubset() != nil {
		routeMetadata = getLbMetadata(nil, destination.GetSubset().GetValues(), "")
	}
	return routeMetadata
}

func checkThatSubsetMatchesUpstream(params plugins.Params, dest *v1.Destination) error {

	// make sure we have a subset config on the route
	if dest.GetSubset() == nil {
		return nil
	}
	if len(dest.GetSubset().GetValues()) == 0 {
		return nil
	}
	routeSubset := dest.GetSubset().GetValues()

	ref, err := usconversion.DestinationToUpstreamRef(dest)
	if err != nil {
		return err
	}

	upstream, err := params.Snapshot.Upstreams.Find(ref.GetNamespace(), ref.GetName())
	if err != nil {
		return pluginutils.NewUpstreamNotFoundErr(*ref)
	}

	subsetConfig := getSubsets(upstream)

	// if a route has a subset config, and an upstream doesnt - its an error
	if subsetConfig == nil {
		return SubsetsMisconfiguredErr
	}

	// make sure that the subset on the route will match a subset on the upstream.
	found := false
Outerloop:
	for _, subset := range subsetConfig.GetSelectors() {
		keys := subset.GetKeys()
		if len(keys) != len(routeSubset) {
			continue
		}
		for _, k := range keys {
			if _, ok := routeSubset[k]; !ok {
				continue Outerloop
			}
		}
		found = true
		break
	}

	if !found {
		return errors.Errorf("route has a subset config, but none of the subsets in the upstream match it")

	}
	return nil
}

func getSubsets(upstream *v1.Upstream) *v1plugins.SubsetSpec {

	specGetter, ok := upstream.GetUpstreamType().(v1.SubsetSpecGetter)
	if !ok {
		return nil
	}
	glooSubsetConfig := specGetter.GetSubsetSpec()

	return glooSubsetConfig

}

func setEnvoyPathMatcher(ctx context.Context, in *matchers.Matcher, out *envoy_config_route_v3.RouteMatch) {
	switch path := in.GetPathSpecifier().(type) {
	case *matchers.Matcher_Exact:
		out.PathSpecifier = &envoy_config_route_v3.RouteMatch_Path{
			Path: path.Exact,
		}
	case *matchers.Matcher_Regex:
		out.PathSpecifier = &envoy_config_route_v3.RouteMatch_SafeRegex{
			SafeRegex: regexutils.NewRegex(ctx, path.Regex),
		}
	case *matchers.Matcher_Prefix:
		out.PathSpecifier = &envoy_config_route_v3.RouteMatch_Prefix{
			Prefix: path.Prefix,
		}
	}
}

func envoyHeaderMatcher(ctx context.Context, in []*matchers.HeaderMatcher) []*envoy_config_route_v3.HeaderMatcher {
	var out []*envoy_config_route_v3.HeaderMatcher
	for _, matcher := range in {

		envoyMatch := &envoy_config_route_v3.HeaderMatcher{
			Name: matcher.GetName(),
		}
		if matcher.GetValue() == "" {
			envoyMatch.HeaderMatchSpecifier = &envoy_config_route_v3.HeaderMatcher_PresentMatch{
				PresentMatch: true,
			}
		} else {
			if matcher.GetRegex() {
				envoyMatch.HeaderMatchSpecifier = &envoy_config_route_v3.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: regexutils.NewRegex(ctx, matcher.GetValue()),
				}
			} else {
				envoyMatch.HeaderMatchSpecifier = &envoy_config_route_v3.HeaderMatcher_ExactMatch{
					ExactMatch: matcher.GetValue(),
				}
			}
		}

		if matcher.GetInvertMatch() {
			envoyMatch.InvertMatch = true
		}

		out = append(out, envoyMatch)
	}
	return out
}

func envoyQueryMatcher(ctx context.Context, in []*matchers.QueryParameterMatcher) []*envoy_config_route_v3.QueryParameterMatcher {
	var out []*envoy_config_route_v3.QueryParameterMatcher
	for _, matcher := range in {
		envoyMatch := &envoy_config_route_v3.QueryParameterMatcher{
			Name: matcher.GetName(),
		}

		if matcher.GetValue() == "" {
			envoyMatch.QueryParameterMatchSpecifier = &envoy_config_route_v3.QueryParameterMatcher_PresentMatch{
				PresentMatch: true,
			}
		} else {
			if matcher.GetRegex() {
				envoyMatch.QueryParameterMatchSpecifier = &envoy_config_route_v3.QueryParameterMatcher_StringMatch{
					StringMatch: &envoy_type_matcher_v3.StringMatcher{
						MatchPattern: &envoy_type_matcher_v3.StringMatcher_SafeRegex{
							SafeRegex: regexutils.NewRegex(ctx, matcher.GetValue()),
						},
					},
				}
			} else {
				envoyMatch.QueryParameterMatchSpecifier = &envoy_config_route_v3.QueryParameterMatcher_StringMatch{
					StringMatch: &envoy_type_matcher_v3.StringMatcher{
						MatchPattern: &envoy_type_matcher_v3.StringMatcher_Exact{
							Exact: matcher.GetValue(),
						},
					},
				}
			}
		}
		out = append(out, envoyMatch)
	}
	return out
}

// returns an error if any of the virtualhost domains overlap
// Visible for testing
func ValidateVirtualHostDomains(virtualHosts []*v1.VirtualHost, httpListenerReport *validationapi.HttpListenerReport) {
	// this shouldbe a 1-1 mapping
	// if len(domainsToVirtualHosts[domain]) > 1, it's an error
	domainsToVirtualHosts := make(map[string][]int)
	for i, vHost := range virtualHosts {
		if len(vHost.GetDomains()) == 0 {
			// default virtualhost
			domainsToVirtualHosts["*"] = append(domainsToVirtualHosts["*"], i)
		}
		for _, domain := range vHost.GetDomains() {
			if domain == "" {
				vhostReport := httpListenerReport.GetVirtualHostReports()[i]
				validation.AppendVirtualHostError(
					vhostReport,
					validationapi.VirtualHostReport_Error_EmptyDomainError,
					fmt.Sprintf("virtual host %s has an empty domain", vHost.GetName()),
				)
			}
			domainsToVirtualHosts[domain] = append(domainsToVirtualHosts[domain], i)
		}
	}
	// see if we found any conflicts, if so, write reports
	for domain, vHosts := range domainsToVirtualHosts {
		if len(vHosts) > 1 {
			var vHostNames []string
			// collect names of all vhosts with the domain
			for _, vHost := range vHosts {
				vHostNames = append(vHostNames, virtualHosts[vHost].GetName())
			}

			// append errors for this vhost
			for _, vHost := range vHosts {
				vhostReport := httpListenerReport.GetVirtualHostReports()[vHost]
				validation.AppendVirtualHostError(
					vhostReport,
					validationapi.VirtualHostReport_Error_DomainsNotUniqueError,
					fmt.Sprintf("domain %v is "+
						"shared by the following virtual hosts: %v", domain, vHostNames),
				)
			}
		}
	}
}

func ValidateRouteDestinations(snap *v1.ApiSnapshot, action *v1.RouteAction) error {
	upstreams := snap.Upstreams
	// make sure the destination itself has the right structure
	switch dest := action.GetDestination().(type) {
	case *v1.RouteAction_Single:
		return validateSingleDestination(upstreams, dest.Single)
	case *v1.RouteAction_Multi:
		return validateMultiDestination(upstreams, dest.Multi.GetDestinations())
	case *v1.RouteAction_UpstreamGroup:
		return validateUpstreamGroup(snap, dest.UpstreamGroup)
	// Cluster Header can not be validated because the cluster name is not provided till runtime
	case *v1.RouteAction_ClusterHeader:
		return validateClusterHeader(action.GetClusterHeader())
	}
	return errors.Errorf("must specify either 'singleDestination', 'multipleDestinations' or 'upstreamGroup' for action")
}

func ValidateTcpRouteDestinations(snap *v1.ApiSnapshot, action *v1.TcpHost_TcpAction) error {
	upstreams := snap.Upstreams
	// make sure the destination itself has the right structure
	switch dest := action.GetDestination().(type) {
	case *v1.TcpHost_TcpAction_Single:
		return validateSingleDestination(upstreams, dest.Single)
	case *v1.TcpHost_TcpAction_Multi:
		return validateMultiDestination(upstreams, dest.Multi.GetDestinations())
	case *v1.TcpHost_TcpAction_UpstreamGroup:
		return validateUpstreamGroup(snap, dest.UpstreamGroup)
	case *v1.TcpHost_TcpAction_ForwardSniClusterName:
		return nil
	}
	return errors.Errorf("must specify either 'singleDestination', 'multipleDestinations', 'upstreamGroup' or 'forwardSniClusterName' for action")
}

func validateUpstreamGroup(snap *v1.ApiSnapshot, ref *core.ResourceRef) error {

	upstreamGroup, err := snap.UpstreamGroups.Find(ref.GetNamespace(), ref.GetName())
	if err != nil {
		return pluginutils.NewUpstreamGroupNotFoundErr(*ref)
	}
	upstreams := snap.Upstreams

	err = validateMultiDestination(upstreams, upstreamGroup.GetDestinations())
	if err != nil {
		return err
	}
	return nil
}

func validateMultiDestination(upstreams []*v1.Upstream, destinations []*v1.WeightedDestination) error {
	for _, dest := range destinations {
		if err := validateSingleDestination(upstreams, dest.GetDestination()); err != nil {
			return errors.Wrap(err, "invalid destination in weighted destination list")
		}
	}
	return nil
}

func validateSingleDestination(upstreams v1.UpstreamList, destination *v1.Destination) error {
	upstreamRef, err := usconversion.DestinationToUpstreamRef(destination)
	if err != nil {
		return err
	}
	_, err = upstreams.Find(upstreamRef.Strings())
	if err != nil {
		return pluginutils.NewUpstreamNotFoundErr(*upstreamRef)
	}
	return nil
}

func validateClusterHeader(header string) error {
	// check that header name is only ASCII characters
	for i := 0; i < len(header); i++ {
		if header[i] > unicode.MaxASCII || header[i] == ':' {
			return fmt.Errorf("%s is an invalid HTTP header name", header)
		}
	}
	return nil
}

func validateListenerSslConfig(params plugins.Params, listener *v1.Listener) error {
	sslCfgTranslator := utils.NewSslConfigTranslator()
	for _, ssl := range listener.GetSslConfigurations() {
		if _, err := sslCfgTranslator.ResolveDownstreamSslConfig(params.Snapshot.Secrets, ssl); err != nil {
			return err
		}
	}
	return nil
}

func DataSourceFromString(str string) *envoy_config_core_v3.DataSource {
	return &envoy_config_core_v3.DataSource{
		Specifier: &envoy_config_core_v3.DataSource_InlineString{
			InlineString: str,
		},
	}
}

func isWarningErr(err error) bool {
	switch {
	case err == SubsetsMisconfiguredErr:
		fallthrough
	case pluginutils.IsDestinationNotFoundErr(err):
		return true
	default:
		return false
	}
}
