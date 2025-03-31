package databaseclaim

import (
	"context"
	"errors"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	"github.com/spf13/viper"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

type MockClient struct {
	client.Client
	GetFunc    func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	ListFunc   func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	UpdateFunc func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error
	CreateFunc func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	DeleteFunc func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
}

func (m *MockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, key, obj, opts...)
	}
	return nil
}

func (m *MockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, list, opts...)
	}
	return nil
}

func (m *MockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, obj, opts...)
	}
	return nil
}

func (m *MockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, obj, opts...)
	}
	return nil
}

func (m *MockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, obj, opts...)
	}
	return nil
}

func Test_providerCRAlreadyExists(t *testing.T) {
	ctx := context.TODO()

	t.Run("CR does exist on the cluster", func(t *testing.T) {
		mockClient := &MockClient{
			GetFunc: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return nil
			},
		}
		reqInfo := &requestInfo{
			HostParams: hostparams.HostParams{},
		}
		dbClaim := &v1.DatabaseClaim{}
		reconciler := &DatabaseClaimReconciler{
			Client: mockClient,
			Config: &DatabaseClaimConfig{
				Viper: viper.New(),
			},
		}

		reconciler.Config.Viper.Set("cloud", "aws")

		exists, err := reconciler.providerCRAlreadyExists(ctx, reqInfo, dbClaim)
		if err != nil {
			t.Fatalf("failed to check if CR exists: %v", err)
		}

		if !exists {
			t.Errorf("expected CR exists")
		}
	})

	t.Run("CR does not exist on the cluster", func(t *testing.T) {
		mockClient := &MockClient{
			GetFunc: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return errors.New("not found")
			},
		}
		reqInfo := &requestInfo{
			HostParams: hostparams.HostParams{},
		}
		dbClaim := &v1.DatabaseClaim{}
		reconciler := &DatabaseClaimReconciler{
			Client: mockClient,
			Config: &DatabaseClaimConfig{
				Viper: viper.New(),
			},
		}

		reconciler.Config.Viper.Set("cloud", "aws")

		exists, err := reconciler.providerCRAlreadyExists(ctx, reqInfo, dbClaim)
		if err != nil {
			t.Fatalf("failed to check if CR exists: %v", err)
		}

		if exists {
			t.Errorf("expected CR does not exists")
		}
	})

	t.Run("Bad viper cloud configuration", func(t *testing.T) {
		reqInfo := &requestInfo{
			HostParams: hostparams.HostParams{},
		}
		dbClaim := &v1.DatabaseClaim{}
		r := &DatabaseClaimReconciler{
			Client: &MockClient{},
			Config: &DatabaseClaimConfig{
				Viper: viper.New(),
			},
		}

		r.Config.Viper.Set("cloud", "anything")

		_, err := r.providerCRAlreadyExists(ctx, reqInfo, dbClaim)
		if err == nil {
			t.Errorf("expected an error but got nil")
		} else if err.Error() != "unsupported cloud providers: anything" {
			t.Errorf("expected error 'unsupported cloud providers: anything', got '%s'", err.Error())
		}
	})
}
