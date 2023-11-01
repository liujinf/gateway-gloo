package bootstrap

import (
	"context"

	"github.com/solo-io/solo-kit/pkg/api/v1/clients/kube/cache"

	consulapi "github.com/hashicorp/consul/api"
	vaultapi "github.com/hashicorp/vault/api"
	v1 "github.com/solo-io/gloo/projects/gloo/pkg/api/v1"
	"github.com/solo-io/gloo/projects/gloo/pkg/bootstrap/clients"
	"github.com/solo-io/gloo/projects/gloo/pkg/defaults"
	"github.com/solo-io/solo-kit/pkg/api/v1/clients/factory"
	"github.com/solo-io/solo-kit/pkg/api/v1/clients/kube"
	"github.com/solo-io/solo-kit/pkg/api/v1/clients/kube/crd"
	"github.com/solo-io/solo-kit/pkg/api/v1/clients/memory"
	skkube "github.com/solo-io/solo-kit/pkg/api/v1/resources/common/kubernetes"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// Deprecated. Use bootstrap/clients.
	DefaultK8sQPS = clients.DefaultK8sQPS // 10x the k8s-recommended default; gloo gets busy writing status updates
	// Deprecated. Use bootstrap/clients.
	DefaultK8sBurst = clients.DefaultK8sBurst // 10x the k8s-recommended default; gloo gets busy writing status updates
	// Deprecated. Use bootstrap/clients.
	DefaultRootKey = clients.DefaultRootKey // used for vault and consul key-value storage
)

// Deprecated. Use bootstrap/clients.
var DefaultQueryOptions = clients.DefaultConsulQueryOptions

// Deprecated. Use bootstrap/clients.
type ConfigFactoryParams = clients.ConfigFactoryParams

// Deprecated. Use bootstrap/clients.
func NewConfigFactoryParams(settings *v1.Settings,
	sharedCache memory.InMemoryResourceCache,
	cache kube.SharedCache,
	cfg **rest.Config,
	consulClient *consulapi.Client) ConfigFactoryParams {
	return clients.NewConfigFactoryParams(settings, sharedCache, cache, cfg, consulClient)
}

// Deprecated. Use bootstrap/clients.
func ConfigFactoryForSettings(params ConfigFactoryParams, resourceCrd crd.Crd) (factory.ResourceClientFactory, error) {
	return clients.ConfigFactoryForSettings(params, resourceCrd)
}

// Deprecated. Use bootstrap/clients.
func KubeServiceClientForSettings(ctx context.Context,
	settings *v1.Settings,
	sharedCache memory.InMemoryResourceCache,
	cfg **rest.Config,
	clientset *kubernetes.Interface,
	kubeCoreCache *cache.KubeCoreCache) (skkube.ServiceClient, error) {
	return clients.KubeServiceClientForSettings(ctx,
		settings,
		sharedCache,
		cfg,
		clientset,
		kubeCoreCache,
	)
}

// Deprecated. Use bootstrap/clients.
func SecretFactoryForSettings(ctx context.Context,
	settings *v1.Settings,
	sharedCache memory.InMemoryResourceCache,
	cfg **rest.Config,
	clientset *kubernetes.Interface,
	kubeCoreCache *cache.KubeCoreCache,
	vaultClient *vaultapi.Client,
	pluralName string) (factory.ResourceClientFactory, error) {
	clientInitMap := map[int]clients.VaultClientInitFunc{clients.SecretSourceAPIVaultClientInitIndex: clients.NoopVaultClientInitFunc(vaultClient)}
	return clients.SecretFactoryForSettings(ctx,
		clients.SecretFactoryParams{
			Settings:           settings,
			SharedCache:        sharedCache,
			Cfg:                cfg,
			Clientset:          clientset,
			KubeCoreCache:      kubeCoreCache,
			VaultClientInitMap: clientInitMap,
			PluralName:         pluralName,
		},
	)
}

// Deprecated. Use bootstrap/clients.
func ArtifactFactoryForSettings(ctx context.Context,
	settings *v1.Settings,
	sharedCache memory.InMemoryResourceCache,
	cfg **rest.Config,
	clientset *kubernetes.Interface,
	kubeCoreCache *cache.KubeCoreCache,
	consulClient *consulapi.Client,
	pluralName string) (factory.ResourceClientFactory, error) {
	return clients.ArtifactFactoryForSettings(ctx,
		settings,
		sharedCache,
		cfg,
		clientset,
		kubeCoreCache,
		consulClient,
		pluralName,
	)
}

// GetWriteNamespace checks the provided settings for the field `DiscoveryNamespace`
// and defaults to `defaults.GlooSystem` if not found.
func GetWriteNamespace(settings *v1.Settings) string {
	writeNamespace := settings.GetDiscoveryNamespace()
	if writeNamespace == "" {
		writeNamespace = defaults.GlooSystem
	}

	return writeNamespace
}
