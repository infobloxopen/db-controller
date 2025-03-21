package providers

import (
	"context"
	"github.com/spf13/viper"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CloudNativePGProvider struct {
}

func newCloudNativePGProvider(k8sClient client.Client, config *viper.Viper, serviceNS string) Provider {
	return &CloudNativePGProvider{}
}

func (p *CloudNativePGProvider) CreateDatabase(ctx context.Context, spec DatabaseSpec) (bool, error) {
	return false, nil
}

func (p *CloudNativePGProvider) DeleteDatabase(ctx context.Context, spec DatabaseSpec) error {
	return nil
}

func (p *CloudNativePGProvider) GetDatabase(ctx context.Context, name string) (*DatabaseSpec, error) {
	return nil, nil
}
