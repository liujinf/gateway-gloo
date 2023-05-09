package translator

import (
	"context"

	"github.com/solo-io/gloo/projects/gloo/pkg/plugins"

	validationapi "github.com/solo-io/gloo/projects/gloo/pkg/api/grpc/validation"
	v1 "github.com/solo-io/gloo/projects/gloo/pkg/api/v1"
	"github.com/solo-io/gloo/projects/gloo/pkg/api/v1/ssl"
	"github.com/solo-io/gloo/projects/gloo/pkg/utils"
	"github.com/solo-io/go-utils/contextutils"
)

// The Listener subsystem handles downstream request processing.
// https://www.envoyproxy.io/docs/envoy/latest/intro/life_of_a_request.html?#high-level-architecture
// Gloo sends resources to Envoy via xDS. The components of the Listener subsystem that Gloo configures are:
// 1. Listeners
// 2. RouteConfiguration
// Given that Gloo exposes a variety of ListenerTypes (HttpListener, TcpListener, HybridListener, AggregateListener), and each of these types
// affect how resources are generated, we abstract those implementation details behind abstract translators.
// The ListenerSubsystemTranslatorFactory returns a ListenerTranslator and RouteConfigurationTranslator for a given Gloo Listener
type ListenerSubsystemTranslatorFactory struct {
	pluginRegistry      plugins.PluginRegistry
	sslConfigTranslator utils.SslConfigTranslator
}

func NewListenerSubsystemTranslatorFactory(
	pluginRegistry plugins.PluginRegistry,
	sslConfigTranslator utils.SslConfigTranslator,
) *ListenerSubsystemTranslatorFactory {
	return &ListenerSubsystemTranslatorFactory{
		pluginRegistry:      pluginRegistry,
		sslConfigTranslator: sslConfigTranslator,
	}
}

func (l *ListenerSubsystemTranslatorFactory) GetTranslators(ctx context.Context, proxy *v1.Proxy, listener *v1.Listener, listenerReport *validationapi.ListenerReport) (
	ListenerTranslator,
	RouteConfigurationTranslator,
) {
	switch listener.GetListenerType().(type) {
	case *v1.Listener_HttpListener:
		return l.GetHttpListenerTranslators(ctx, proxy, listener, listenerReport)

	case *v1.Listener_TcpListener:
		return l.GetTcpListenerTranslators(ctx, listener, listenerReport)

	case *v1.Listener_HybridListener:
		return l.GetHybridListenerTranslators(ctx, proxy, listener, listenerReport)

	case *v1.Listener_AggregateListener:
		return l.GetAggregateListenerTranslators(ctx, proxy, listener, listenerReport)
	default:
		// This case should never occur
		return &emptyListenerTranslator{}, &emptyRouteConfigurationTranslator{}
	}
}

func (l *ListenerSubsystemTranslatorFactory) GetHttpListenerTranslators(ctx context.Context, proxy *v1.Proxy, listener *v1.Listener, listenerReport *validationapi.ListenerReport) (
	ListenerTranslator,
	RouteConfigurationTranslator,
) {
	httpListenerReport := listenerReport.GetHttpListenerReport()
	if httpListenerReport == nil {
		contextutils.LoggerFrom(ctx).DPanic("internal error: listener report was not http type")
	}

	// The routeConfigurationName is used to match the RouteConfiguration
	// to an implementation of the HttpConnectionManager NetworkFilter
	// https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/rds#config-http-conn-man-rds
	routeConfigurationName := utils.RouteConfigName(listener)

	// This translator produces NetworkFilters
	// Most notably, this includes the HttpConnectionManager NetworkFilter
	networkFilterTranslator := NewHttpListenerNetworkFilterTranslator(
		listener,
		listener.GetHttpListener(),
		httpListenerReport,
		l.pluginRegistry.GetHttpFilterPlugins(),
		l.pluginRegistry.GetHttpConnectionManagerPlugins(),
		routeConfigurationName)

	// This translator produces FilterChains
	// For an HttpGateway we first build a set of NetworkFilters.
	// Then, for each SslConfiguration that was found on that HttpGateway,
	// we create a replica of the FilterChain, just with a different FilterChainMatcher
	filterChainTranslator := &httpFilterChainTranslator{
		parentReport:            listenerReport,
		networkFilterTranslator: networkFilterTranslator,
		sslConfigTranslator:     l.sslConfigTranslator,
		sslConfigurations:       listener.GetSslConfigurations(),
		defaultSslConfig:        nil, // not available for HttpGateway, HybridGateway only feature
		sourcePrefixRanges:      nil, // not available for HttpGateway, HybridGateway only feature
	}

	// This translator produces a single Listener
	listenerTranslator := &listenerTranslatorInstance{
		listener:              listener,
		report:                listenerReport,
		plugins:               l.pluginRegistry.GetListenerPlugins(),
		filterChainTranslator: filterChainTranslator,
	}

	// This translator produces a single RouteConfiguration
	// We produce the same number of RouteConfigurations as we do
	// unique instances of the HttpConnectionManager NetworkFilter
	// Since an HttpGateway can only be configured with a single set
	// of configuration for the HttpConnectionManager, we only produce
	// a single RouteConfiguration
	routeConfigurationTranslator := &httpRouteConfigurationTranslator{
		pluginRegistry:           l.pluginRegistry,
		proxy:                    proxy,
		parentListener:           listener,
		listener:                 listener.GetHttpListener(),
		parentReport:             listenerReport,
		report:                   httpListenerReport,
		routeConfigName:          routeConfigurationName,
		requireTlsOnVirtualHosts: len(listener.GetSslConfigurations()) > 0,
	}

	return listenerTranslator, routeConfigurationTranslator
}

func (l *ListenerSubsystemTranslatorFactory) GetTcpListenerTranslators(ctx context.Context, listener *v1.Listener, listenerReport *validationapi.ListenerReport) (
	ListenerTranslator,
	RouteConfigurationTranslator,
) {
	tcpListenerReport := listenerReport.GetTcpListenerReport()
	if tcpListenerReport == nil {
		contextutils.LoggerFrom(ctx).DPanic("internal error: listener report was not tcp type")
	}

	// This translator produces FilterChains
	// Our current TcpFilterChainPlugins have a 1-many relationship,
	// meaning that a single TcpListener produces many FilterChains
	filterChainTranslator := &tcpFilterChainTranslator{
		plugins:            l.pluginRegistry.GetTcpFilterChainPlugins(),
		parentListener:     listener,
		listener:           listener.GetTcpListener(),
		report:             tcpListenerReport,
		sourcePrefixRanges: nil, // not available for TcpGateway, HybridGateway only feature
	}

	// This translator produces a single Listener
	listenerTranslator := &listenerTranslatorInstance{
		listener:              listener,
		report:                listenerReport,
		plugins:               l.pluginRegistry.GetListenerPlugins(),
		filterChainTranslator: filterChainTranslator,
	}

	// A TcpListener does not produce any RouteConfiguration
	routeConfigurationTranslator := &emptyRouteConfigurationTranslator{}

	return listenerTranslator, routeConfigurationTranslator
}

func (l *ListenerSubsystemTranslatorFactory) GetHybridListenerTranslators(ctx context.Context, proxy *v1.Proxy, listener *v1.Listener, listenerReport *validationapi.ListenerReport) (
	ListenerTranslator,
	RouteConfigurationTranslator,
) {
	hybridListenerReport := listenerReport.GetHybridListenerReport()
	if hybridListenerReport == nil {
		contextutils.LoggerFrom(ctx).DPanic("internal error: listener report was not hybrid type")
		return nil, nil
	}

	// HybridListeners are just an abstraction of HttpListeners and TcpListeners
	// The main distinction is that they support N FilterChains instead of just 1
	// Therefore, we iterate over each MatchedListener on a HybridGateway, produce
	// the relevant translators and aggregate them together
	var routeConfigurationTranslators []RouteConfigurationTranslator
	var filterChainTranslators []FilterChainTranslator

	for _, matchedListener := range listener.GetHybridListener().GetMatchedListeners() {
		var (
			routeConfigurationTranslator RouteConfigurationTranslator
			filterChainTranslator        FilterChainTranslator
		)
		matcher := matchedListener.GetMatcher()

		switch listenerType := matchedListener.GetListenerType().(type) {
		case *v1.MatchedListener_HttpListener:
			// The routeConfigurationName is used to match the RouteConfiguration
			// to an implementation of the HttpConnectionManager NetworkFilter
			// https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/rds#config-http-conn-man-rds
			routeConfigurationName := utils.MatchedRouteConfigName(listener, matcher)

			httpListenerReport := hybridListenerReport.GetMatchedListenerReports()[routeConfigurationName].GetHttpListenerReport()

			// This translator produces NetworkFilters
			// Most notably, this includes the HttpConnectionManager NetworkFilter
			networkFilterTranslator := NewHttpListenerNetworkFilterTranslator(
				listener,
				listenerType.HttpListener,
				httpListenerReport,
				l.pluginRegistry.GetHttpFilterPlugins(),
				l.pluginRegistry.GetHttpConnectionManagerPlugins(),
				routeConfigurationName)

			// This translator produces FilterChains
			// For an HttpGateway we first build a set of NetworkFilters.
			// Then, for each SslConfiguration that was found on that HttpGateway,
			// we create a replica of the FilterChain, just with a different FilterChainMatcher
			filterChainTranslator = &httpFilterChainTranslator{
				parentReport:            listenerReport,
				networkFilterTranslator: networkFilterTranslator,
				sslConfigTranslator:     l.sslConfigTranslator,
				sslConfigurations:       matchedListener.GetSslConfigurations(),
				defaultSslConfig:        matcher.GetSslConfig(),          // HybridGateway only feature
				sourcePrefixRanges:      matcher.GetSourcePrefixRanges(), // HybridGateway only feature
			}

			// This translator produces a single RouteConfiguration
			// We produce the same number of RouteConfigurations as we do
			// unique instances of the HttpConnectionManager NetworkFilter
			routeConfigurationTranslator = &httpRouteConfigurationTranslator{
				pluginRegistry:           l.pluginRegistry,
				proxy:                    proxy,
				parentListener:           listener,
				listener:                 listenerType.HttpListener,
				parentReport:             listenerReport,
				report:                   httpListenerReport,
				routeConfigName:          routeConfigurationName,
				requireTlsOnVirtualHosts: matcher.GetSslConfig() != nil,
			}

		case *v1.MatchedListener_TcpListener:
			// This translator produces FilterChains
			// Our current TcpFilterChainPlugins have a 1-many relationship,
			// meaning that a single TcpListener produces many FilterChains
			filterChainTranslator = &tcpFilterChainTranslator{
				plugins:            l.pluginRegistry.GetTcpFilterChainPlugins(),
				parentListener:     listener,
				listener:           listenerType.TcpListener,
				report:             hybridListenerReport.GetMatchedListenerReports()[utils.MatchedRouteConfigName(listener, matcher)].GetTcpListenerReport(),
				sourcePrefixRanges: matcher.GetSourcePrefixRanges(), // HybridGateway only feature
			}

			// A TcpListener does not produce any RouteConfiguration
			routeConfigurationTranslator = &emptyRouteConfigurationTranslator{}
		}

		filterChainTranslators = append(filterChainTranslators, filterChainTranslator)
		routeConfigurationTranslators = append(routeConfigurationTranslators, routeConfigurationTranslator)
	}

	listenerTranslator := &listenerTranslatorInstance{
		listener: listener,
		report:   listenerReport,
		plugins:  l.pluginRegistry.GetListenerPlugins(),
		filterChainTranslator: &multiFilterChainTranslator{
			translators: filterChainTranslators,
		},
	}

	routeConfigurationTranslator := &multiRouteConfigurationTranslator{
		translators: routeConfigurationTranslators,
	}

	return listenerTranslator, routeConfigurationTranslator
}

func (l *ListenerSubsystemTranslatorFactory) GetAggregateListenerTranslators(ctx context.Context, proxy *v1.Proxy, listener *v1.Listener, listenerReport *validationapi.ListenerReport) (
	ListenerTranslator,
	RouteConfigurationTranslator,
) {
	aggregateListenerReport := listenerReport.GetAggregateListenerReport()
	if aggregateListenerReport == nil {
		contextutils.LoggerFrom(ctx).DPanic("internal error: listener report was not aggregate type")
		return nil, nil
	}

	var routeConfigurationTranslators []RouteConfigurationTranslator
	var filterChainTranslators []FilterChainTranslator

	httpResources := listener.GetAggregateListener().GetHttpResources()

	for _, httpFilterChain := range listener.GetAggregateListener().GetHttpFilterChains() {
		var (
			routeConfigurationTranslator RouteConfigurationTranslator
			filterChainTranslator        FilterChainTranslator
		)

		// The routeConfigurationName is used to match the RouteConfiguration
		// to an implementation of the HttpConnectionManager NetworkFilter
		// https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/rds#config-http-conn-man-rds
		routeConfigurationName := utils.MatchedRouteConfigName(listener, httpFilterChain.GetMatcher())

		// Build the HttpListener from the refs defined on the HttpFilterChain
		httpListener := &v1.HttpListener{
			Options: httpResources.GetHttpOptions()[httpFilterChain.GetHttpOptionsRef()],
		}
		for _, vhostRef := range httpFilterChain.GetVirtualHostRefs() {
			httpListener.VirtualHosts = append(httpListener.GetVirtualHosts(), httpResources.GetVirtualHosts()[vhostRef])
		}

		httpListenerReport := aggregateListenerReport.GetHttpListenerReports()[routeConfigurationName]

		// This translator produces NetworkFilters
		// Most notably, this includes the HttpConnectionManager NetworkFilter
		networkFilterTranslator := NewHttpListenerNetworkFilterTranslator(
			listener,
			httpListener,
			httpListenerReport,
			l.pluginRegistry.GetHttpFilterPlugins(),
			l.pluginRegistry.GetHttpConnectionManagerPlugins(),
			routeConfigurationName)

		// This translator produces FilterChains
		// For an HttpGateway we first build a set of NetworkFilters.
		// Then, for each SslConfiguration that was found on that HttpGateway,
		// we create a replica of the FilterChain, just with a different FilterChainMatcher
		filterChainTranslator = &httpFilterChainTranslator{
			parentReport:            listenerReport,
			networkFilterTranslator: networkFilterTranslator,
			sslConfigTranslator:     l.sslConfigTranslator,
			sslConfigurations:       []*ssl.SslConfig{httpFilterChain.GetMatcher().GetSslConfig()},
			defaultSslConfig:        nil,
			sourcePrefixRanges:      httpFilterChain.GetMatcher().GetSourcePrefixRanges(),
		}

		// This translator produces a single RouteConfiguration
		// We produce the same number of RouteConfigurations as we do
		// unique instances of the HttpConnectionManager NetworkFilter
		routeConfigurationTranslator = &httpRouteConfigurationTranslator{
			pluginRegistry:           l.pluginRegistry,
			proxy:                    proxy,
			parentListener:           listener,
			listener:                 httpListener,
			parentReport:             listenerReport,
			report:                   httpListenerReport,
			routeConfigName:          routeConfigurationName,
			requireTlsOnVirtualHosts: httpFilterChain.GetMatcher().GetSslConfig() != nil,
		}

		filterChainTranslators = append(filterChainTranslators, filterChainTranslator)
		routeConfigurationTranslators = append(routeConfigurationTranslators, routeConfigurationTranslator)
	}

	listenerTranslator := &listenerTranslatorInstance{
		listener: listener,
		report:   listenerReport,
		plugins:  l.pluginRegistry.GetListenerPlugins(),
		filterChainTranslator: &multiFilterChainTranslator{
			translators: filterChainTranslators,
		},
	}

	routeConfigurationTranslator := &multiRouteConfigurationTranslator{
		translators: routeConfigurationTranslators,
	}

	return listenerTranslator, routeConfigurationTranslator
}
