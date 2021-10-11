package upstreamconn

import (
	"math"

	envoy_config_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/rotisserie/eris"
	v1 "github.com/solo-io/gloo/projects/gloo/pkg/api/v1"
	"github.com/solo-io/gloo/projects/gloo/pkg/plugins"
	"github.com/solo-io/solo-kit/pkg/utils/prototime"
)

var _ plugins.Plugin = new(Plugin)
var _ plugins.UpstreamPlugin = new(Plugin)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Init(params plugins.InitParams) error {
	return nil
}

func (p *Plugin) ProcessUpstream(params plugins.Params, in *v1.Upstream, out *envoy_config_cluster_v3.Cluster) error {

	cfg := in.GetConnectionConfig()
	if cfg == nil {
		return nil
	}

	if cfg.GetMaxRequestsPerConnection() > 0 {
		out.MaxRequestsPerConnection = &wrappers.UInt32Value{
			Value: cfg.GetMaxRequestsPerConnection(),
		}
	}

	if cfg.GetConnectTimeout() != nil {
		out.ConnectTimeout = cfg.GetConnectTimeout()
	}

	if cfg.GetTcpKeepalive() != nil {
		out.UpstreamConnectionOptions = &envoy_config_cluster_v3.UpstreamConnectionOptions{
			TcpKeepalive: convertTcpKeepAlive(cfg.GetTcpKeepalive()),
		}
	}

	if cfg.GetPerConnectionBufferLimitBytes() != nil {
		out.PerConnectionBufferLimitBytes = cfg.GetPerConnectionBufferLimitBytes()
	}

	if cfg.GetCommonHttpProtocolOptions() != nil {
		commonHttpProtocolOptions, err := convertHttpProtocolOptions(cfg.GetCommonHttpProtocolOptions())
		if err != nil {
			return err
		}
		out.CommonHttpProtocolOptions = commonHttpProtocolOptions
	}

	return nil
}

func convertTcpKeepAlive(tcp *v1.ConnectionConfig_TcpKeepAlive) *envoy_config_core_v3.TcpKeepalive {
	var probes *wrappers.UInt32Value
	if tcp.GetKeepaliveProbes() > 0 {
		probes = &wrappers.UInt32Value{
			Value: tcp.GetKeepaliveProbes(),
		}
	}
	return &envoy_config_core_v3.TcpKeepalive{
		KeepaliveInterval: roundToSecond(tcp.GetKeepaliveInterval()),
		KeepaliveTime:     roundToSecond(tcp.GetKeepaliveTime()),
		KeepaliveProbes:   probes,
	}
}

func convertHttpProtocolOptions(hpo *v1.ConnectionConfig_HttpProtocolOptions) (*envoy_config_core_v3.HttpProtocolOptions, error) {
	out := &envoy_config_core_v3.HttpProtocolOptions{}

	if hpo.GetIdleTimeout() != nil {
		out.IdleTimeout = hpo.GetIdleTimeout()
	}

	if hpo.GetMaxHeadersCount() > 0 { // Envoy requires this to be >= 1
		out.MaxHeadersCount = &wrappers.UInt32Value{Value: hpo.GetMaxHeadersCount()}
	}

	if hpo.GetMaxStreamDuration() != nil {
		out.MaxStreamDuration = hpo.GetMaxStreamDuration()
	}

	switch hpo.GetHeadersWithUnderscoresAction() {
	case v1.ConnectionConfig_HttpProtocolOptions_ALLOW:
		out.HeadersWithUnderscoresAction = envoy_config_core_v3.HttpProtocolOptions_ALLOW
	case v1.ConnectionConfig_HttpProtocolOptions_REJECT_REQUEST:
		out.HeadersWithUnderscoresAction = envoy_config_core_v3.HttpProtocolOptions_REJECT_REQUEST
	case v1.ConnectionConfig_HttpProtocolOptions_DROP_HEADER:
		out.HeadersWithUnderscoresAction = envoy_config_core_v3.HttpProtocolOptions_DROP_HEADER
	default:
		return &envoy_config_core_v3.HttpProtocolOptions{},
			eris.Errorf("invalid HeadersWithUnderscoresAction %v in CommonHttpProtocolOptions", hpo.GetHeadersWithUnderscoresAction())
	}

	return out, nil
}

func roundToSecond(d *duration.Duration) *wrappers.UInt32Value {
	if d == nil {
		return nil
	}

	// round up
	seconds := math.Round(prototime.DurationFromProto(d).Seconds() + 0.4999)
	return &wrappers.UInt32Value{
		Value: uint32(seconds),
	}

}
