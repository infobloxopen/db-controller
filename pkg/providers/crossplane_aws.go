package providers

import (
	"context"
	"github.com/spf13/viper"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AWSProvider struct {
}

func newAWSProvider(k8sClient client.Client, config *viper.Viper, serviceNS string) Provider {
	return &AWSProvider{}
}

func (p *AWSProvider) CreateDatabase(ctx context.Context, spec DatabaseSpec) (bool, error) {
	return false, nil
}

func (p *AWSProvider) DeleteDatabase(ctx context.Context, spec DatabaseSpec) error {
	return nil
}

func (p *AWSProvider) GetDatabase(ctx context.Context, name string) (*DatabaseSpec, error) {
	return nil, nil
}
