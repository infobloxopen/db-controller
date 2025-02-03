package databaseclaim

import (
	"context"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	"github.com/infobloxopen/db-controller/pkg/roleclaim"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

type MockReader struct {
	mock.Mock
}

func (m *MockReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	args := m.Called(ctx, key, obj, opts)
	return args.Error(0)
}

func (m *MockReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	args := m.Called(ctx, list, opts)
	return args.Error(0)
}

type FakeDB struct{ client.Object }

func Test_crExists(t *testing.T) {
	ctx := context.TODO()
	dbHostName := "test-db"

	tests := []struct {
		name           string
		clientErr      error
		expectedResult bool
	}{
		{
			name:           "CR does not exists on the cluster",
			clientErr:      errors.NewNotFound(schema.GroupResource{Resource: "DBCluster"}, "not found"),
			expectedResult: false,
		},
		{
			name:           "CR does exists on the cluster",
			clientErr:      nil,
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := new(MockReader)
			fakeDbCr := &FakeDB{}
			mockClient.On("Get", ctx, client.ObjectKey{Name: dbHostName}, fakeDbCr, mock.Anything).Return(tt.clientErr).Once()

			result := crExists(ctx, mockClient, dbHostName, fakeDbCr)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func Test_providerCRAlreadyExists(t *testing.T) {
	ctx := context.TODO()

	reqInfo := &requestInfo{
		HostParams: hostparams.HostParams{},
	}
	dbClaim := &v1.DatabaseClaim{}
	r := &DatabaseClaimReconciler{
		Client: &roleclaim.MockClient{},
		Config: &DatabaseClaimConfig{
			Viper: viper.New(),
		},
	}

	r.Config.Viper.Set("cloud", "anything")

	_, err := r.providerCRAlreadyExists(ctx, reqInfo, dbClaim)
	assert.Error(t, err)
	assert.EqualError(t, err, "unsupported cloud provider: anything")
}
