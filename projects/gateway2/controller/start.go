package controller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	apiv1 "sigs.k8s.io/gateway-api/apis/v1"

	gatewayv1 "github.com/solo-io/gloo/projects/gateway/pkg/api/v1"
	"github.com/solo-io/gloo/projects/gateway2/controller/scheme"
	"github.com/solo-io/gloo/projects/gateway2/extensions"
	"github.com/solo-io/gloo/projects/gateway2/proxy_syncer"
	"github.com/solo-io/gloo/projects/gateway2/secrets"
	"github.com/solo-io/gloo/projects/gateway2/wellknown"
	v1 "github.com/solo-io/gloo/projects/gloo/pkg/api/v1"
	api "github.com/solo-io/gloo/projects/gloo/pkg/api/v1/enterprise/options/extauth/v1"
	"github.com/solo-io/gloo/projects/gloo/pkg/bootstrap"
	"github.com/solo-io/gloo/projects/gloo/pkg/plugins"
	"github.com/solo-io/gloo/projects/gloo/pkg/translator"
	"github.com/solo-io/solo-kit/pkg/api/v2/reporter"
)

const (
	// AutoProvision controls whether the controller will be responsible for provisioning dynamic
	// infrastructure for the Gateway API.
	AutoProvision = true
)

var (
	gatewayClass = apiv1.ObjectName(wellknown.GatewayClassName)

	setupLog = ctrl.Log.WithName("setup")
)

type StartConfig struct {
	Dev  bool
	Opts bootstrap.Opts

	// ExtensionsFactory is the factory function which will return an extensions.K8sGatewayExtensions
	// This is responsible for producing the extension points that this controller requires
	ExtensionsFactory extensions.K8sGatewayExtensionsFactory

	// GlooPluginRegistryFactory is the factory function to produce a PluginRegistry
	// The plugins in this registry are used during the conversion of a Proxy resource into an xDS Snapshot
	GlooPluginRegistryFactory plugins.PluginRegistryFactory

	// ProxyClient is the client that writes Proxy resources into an in-memory cache
	// This cache is utilized by the debug.ProxyEndpointServer
	ProxyClient v1.ProxyClient

	// AuthConfigClient is the client used for retrieving AuthConfig objects within the Portal Plugin
	AuthConfigClient api.AuthConfigClient

	// RouteOptionClient is the client used for retrieving RouteOption objects within the RouteOptionsPlugin
	// NOTE: We may be able to move this entirely to the RouteOptionsPlugin
	RouteOptionClient gatewayv1.RouteOptionClient
	// StatusReporter is used within any StatusPlugins that must persist a GE-classic style status
	StatusReporter reporter.StatusReporter
}

// Start runs the controllers responsible for processing the K8s Gateway API objects
// It is intended to be run in a goroutine as the function will block until the supplied
// context is cancelled
func Start(ctx context.Context, cfg StartConfig) error {
	var opts []zap.Opts
	if cfg.Dev {
		setupLog.Info("starting log in dev mode")
		opts = append(opts, zap.UseDevMode(true))
	}
	ctrl.SetLogger(zap.New(opts...))

	mgrOpts := ctrl.Options{
		Scheme:           scheme.NewScheme(),
		PprofBindAddress: "127.0.0.1:9099",
		// if you change the port here, also change the port "health" in the helmchart.
		HealthProbeBindAddress: ":9093",
		Metrics: metricsserver.Options{
			BindAddress: ":9092",
		},
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	// TODO: replace this with something that checks that we have xds snapshot ready (or that we don't need one).
	mgr.AddReadyzCheck("ready-ping", healthz.Ping)

	glooTranslator := translator.NewDefaultTranslator(
		cfg.Opts.Settings,
		cfg.GlooPluginRegistryFactory(ctx))

	inputChannels := proxy_syncer.NewGatewayInputChannels()

	k8sGwExtensions, err := cfg.ExtensionsFactory(ctx, extensions.K8sGatewayExtensionsFactoryParameters{
		Mgr:               mgr,
		AuthConfigClient:  cfg.AuthConfigClient,
		RouteOptionClient: cfg.RouteOptionClient,
		StatusReporter:    cfg.StatusReporter,
		KickXds:           inputChannels.Kick,
	})
	if err != nil {
		setupLog.Error(err, "unable to create k8s gw extensions")
		return err
	}

	// Create the proxy syncer for the Gateway API resources
	proxySyncer := proxy_syncer.NewProxySyncer(
		wellknown.GatewayControllerName,
		cfg.Opts.WriteNamespace,
		glooTranslator,
		inputChannels,
		mgr,
		k8sGwExtensions,
		cfg.ProxyClient,
	)
	if err := mgr.Add(proxySyncer); err != nil {
		setupLog.Error(err, "unable to add proxySyncer runnable")
		return err
	}

	gwCfg := GatewayConfig{
		Mgr:            mgr,
		GWClass:        gatewayClass,
		ControllerName: wellknown.GatewayControllerName,
		AutoProvision:  AutoProvision,
		ControlPlane:   cfg.Opts.ControlPlane,
		IstioValues:    cfg.Opts.GlooGateway.IstioValues,
		Kick:           inputChannels.Kick,
		Extensions:     k8sGwExtensions,
	}
	if err = NewBaseGatewayController(ctx, gwCfg); err != nil {
		setupLog.Error(err, "unable to create controller")
		return err
	}

	if err = secrets.NewSecretsController(ctx, mgr, inputChannels); err != nil {
		setupLog.Error(err, "unable to create controller")
		return err
	}

	return mgr.Start(ctx)
}
