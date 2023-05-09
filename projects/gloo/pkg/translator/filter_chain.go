package translator

import (
	"context"
	"fmt"

	"github.com/solo-io/go-utils/contextutils"

	v3 "github.com/solo-io/gloo/projects/gloo/pkg/api/external/envoy/config/core/v3"
	"github.com/solo-io/gloo/projects/gloo/pkg/api/v1/ssl"

	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyauth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes/duration"

	envoy_config_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/golang/protobuf/proto"
	validationapi "github.com/solo-io/gloo/projects/gloo/pkg/api/grpc/validation"
	v1 "github.com/solo-io/gloo/projects/gloo/pkg/api/v1"
	v1snap "github.com/solo-io/gloo/projects/gloo/pkg/api/v1/gloosnapshot"
	"github.com/solo-io/gloo/projects/gloo/pkg/plugins"
	"github.com/solo-io/gloo/projects/gloo/pkg/utils"
	"github.com/solo-io/gloo/projects/gloo/pkg/utils/validation"
)

type FilterChainTranslator interface {
	ComputeFilterChains(params plugins.Params) []*envoy_config_listener_v3.FilterChain
}

var _ FilterChainTranslator = new(tcpFilterChainTranslator)
var _ FilterChainTranslator = new(httpFilterChainTranslator)

type tcpFilterChainTranslator struct {
	// List of TcpFilterChainPlugins to process
	plugins []plugins.TcpFilterChainPlugin
	// The parent Listener, this is only used to associate errors with the parent resource
	parentListener *v1.Listener
	// The TcpListener used to generate the list of FilterChains
	listener *v1.TcpListener
	// The report used to store processing errors
	report *validationapi.TcpListenerReport

	// These values are optional (currently only available for HybridGateways)
	sourcePrefixRanges []*v3.CidrRange
}

func (t *tcpFilterChainTranslator) ComputeFilterChains(params plugins.Params) []*envoy_config_listener_v3.FilterChain {
	var filterChains []*envoy_config_listener_v3.FilterChain

	// 1. Run the tcp filter chain plugins
	for _, plug := range t.plugins {
		pluginFilterChains, err := plug.CreateTcpFilterChains(params, t.parentListener, t.listener)
		if err != nil {
			validation.AppendTCPListenerError(t.report,
				validationapi.TcpListenerReport_Error_ProcessingError,
				fmt.Sprintf("listener %s: %s", t.parentListener.GetName(), err.Error()))
			continue
		}

		filterChains = append(filterChains, pluginFilterChains...)
	}

	// 2. Apply SourcePrefixRange to FilterChainMatch, if defined
	if len(t.sourcePrefixRanges) > 0 {
		for _, fc := range filterChains {
			applySourcePrefixRangesToFilterChain(fc, t.sourcePrefixRanges)
		}
	}

	return filterChains
}

// An httpFilterChainTranslator configures a single set of NetworkFilters
// and then creates duplicate filter chains for each provided SslConfig.
type httpFilterChainTranslator struct {
	parentReport            *validationapi.ListenerReport
	networkFilterTranslator NetworkFilterTranslator
	sslConfigurations       []*ssl.SslConfig
	sslConfigTranslator     utils.SslConfigTranslator

	// These values are optional (currently only available for HybridListeners or AggregateListeners)
	defaultSslConfig   *ssl.SslConfig
	sourcePrefixRanges []*v3.CidrRange
}

func (h *httpFilterChainTranslator) ComputeFilterChains(params plugins.Params) []*envoy_config_listener_v3.FilterChain {
	// 1. Generate all the network filters (including the HttpConnectionManager)
	networkFilters, err := h.networkFilterTranslator.ComputeNetworkFilters(params)
	if err != nil {
		contextutils.LoggerFrom(context.Background()).DPanic(err)
		return nil
	}
	if len(networkFilters) == 0 {
		return nil
	}

	// 2. Determine which, if any, SslConfigs are defined for this Listener
	sslConfigWithDefaults := h.getSslConfigurationWithDefaults()

	// 3. Create duplicate FilterChains for each unique SslConfig
	filterChains := h.createFilterChainsFromSslConfiguration(params.Snapshot, networkFilters, sslConfigWithDefaults)

	// 4. Apply SourcePrefixRange to FilterChainMatch, if defined
	if len(h.sourcePrefixRanges) > 0 {
		for _, fc := range filterChains {
			applySourcePrefixRangesToFilterChain(fc, h.sourcePrefixRanges)
		}
	}

	return filterChains
}

func (h *httpFilterChainTranslator) getSslConfigurationWithDefaults() []*ssl.SslConfig {
	mergedSslConfigurations := ConsolidateSslConfigurations(h.sslConfigurations)

	if h.defaultSslConfig == nil {
		return mergedSslConfigurations
	}

	// Merge each sslConfig with the default values
	var sslConfigWithDefaults []*ssl.SslConfig
	for _, ssl := range mergedSslConfigurations {
		sslConfigWithDefaults = append(sslConfigWithDefaults, MergeSslConfig(ssl, h.defaultSslConfig))
	}
	return sslConfigWithDefaults
}

func (h *httpFilterChainTranslator) createFilterChainsFromSslConfiguration(
	snap *v1snap.ApiSnapshot,
	networkFilters []*envoy_config_listener_v3.Filter,
	sslConfigurations []*ssl.SslConfig,
) []*envoy_config_listener_v3.FilterChain {

	// if no ssl config is provided, return a single insecure filter chain
	if len(sslConfigurations) == 0 {
		return []*envoy_config_listener_v3.FilterChain{{
			Filters: networkFilters,
		}}
	}

	// create a duplicate of the listener filter chain for each ssl cert we want to serve
	var secureFilterChains []*envoy_config_listener_v3.FilterChain
	for _, sslConfig := range sslConfigurations {
		// get secrets
		downstreamTlsContext, err := h.sslConfigTranslator.ResolveDownstreamSslConfig(snap.Secrets, sslConfig)
		if err != nil {
			validation.AppendListenerError(h.parentReport, validationapi.ListenerReport_Error_SSLConfigError, err.Error())
			continue
		}

		filterChain, err := newSslFilterChain(
			downstreamTlsContext,
			sslConfig.GetSniDomains(),
			networkFilters,
			sslConfig.GetTransportSocketConnectTimeout())
		if err != nil {
			validation.AppendListenerError(h.parentReport, validationapi.ListenerReport_Error_SSLConfigError, err.Error())
			continue
		}
		secureFilterChains = append(secureFilterChains, filterChain)
	}
	return secureFilterChains
}

func applySourcePrefixRangesToFilterChain(
	filterChain *envoy_config_listener_v3.FilterChain,
	sourcePrefixRanges []*v3.CidrRange,
) {
	if filterChain == nil || len(sourcePrefixRanges) == 0 {
		// nothing to do
		return
	}

	if filterChain.GetFilterChainMatch() == nil {
		// create a FilterChainMatch, if necessary
		filterChain.FilterChainMatch = &envoy_config_listener_v3.FilterChainMatch{}
	}

	envoySourcePrefixRanges := make([]*envoy_config_core_v3.CidrRange, len(sourcePrefixRanges))
	for idx, spr := range sourcePrefixRanges {
		outSpr := &envoy_config_core_v3.CidrRange{
			AddressPrefix: spr.GetAddressPrefix(),
			PrefixLen:     spr.GetPrefixLen(),
		}
		envoySourcePrefixRanges[idx] = outSpr
	}

	filterChain.GetFilterChainMatch().SourcePrefixRanges = envoySourcePrefixRanges
}

func newSslFilterChain(
	downstreamTlsContext *envoyauth.DownstreamTlsContext,
	sniDomains []string,
	listenerFilters []*envoy_config_listener_v3.Filter,
	timeout *duration.Duration,
) (*envoy_config_listener_v3.FilterChain, error) {

	// copy listenerFilter so we can modify filter chain later without changing the filters on all of them!
	clonedListenerFilters := cloneListenerFilters(listenerFilters)
	typedConfig, err := utils.MessageToAny(downstreamTlsContext)
	if err != nil {
		return nil, err
	}
	return &envoy_config_listener_v3.FilterChain{
		FilterChainMatch: &envoy_config_listener_v3.FilterChainMatch{
			ServerNames: sniDomains,
		},
		Filters: clonedListenerFilters,
		TransportSocket: &envoy_config_core_v3.TransportSocket{
			Name:       wellknown.TransportSocketTls,
			ConfigType: &envoy_config_core_v3.TransportSocket_TypedConfig{TypedConfig: typedConfig},
		},
		TransportSocketConnectTimeout: timeout,
	}, nil
}

func cloneListenerFilters(originalListenerFilters []*envoy_config_listener_v3.Filter) []*envoy_config_listener_v3.Filter {
	clonedListenerFilters := make([]*envoy_config_listener_v3.Filter, len(originalListenerFilters))
	for i, lf := range originalListenerFilters {
		clonedListenerFilters[i] = proto.Clone(lf).(*envoy_config_listener_v3.Filter)
	}

	return clonedListenerFilters
}

type multiFilterChainTranslator struct {
	translators []FilterChainTranslator
}

func (m *multiFilterChainTranslator) ComputeFilterChains(params plugins.Params) []*envoy_config_listener_v3.FilterChain {
	var outFilterChains []*envoy_config_listener_v3.FilterChain

	for _, translator := range m.translators {
		newFilterChains := translator.ComputeFilterChains(params)
		if newFilterChains != nil {
			outFilterChains = append(outFilterChains, newFilterChains...)
		}
	}

	return outFilterChains
}
