package providers

import (
	"context"
	"github.com/spf13/viper"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type GCPProvider struct {
}

func newGCPProvider(k8sClient client.Client, config *viper.Viper) Provider {
	return &GCPProvider{}
}

func (p *GCPProvider) CreateDatabase(ctx context.Context, spec DatabaseSpec) (bool, error) {
	return false, nil
}

func (p *GCPProvider) DeleteDatabase(ctx context.Context, spec DatabaseSpec) error {
	return nil
}

func (p *GCPProvider) GetDatabase(ctx context.Context, name string) (*DatabaseSpec, error) {
	return nil, nil
}
