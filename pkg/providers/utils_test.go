package providers

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"testing"

	. "github.com/onsi/gomega"
)

func TestGetParameterGroupName(t *testing.T) {
	tests := []struct {
		name                  string
		providerResourceName  string
		dbVersion             string
		dbType                string
		expectedParameterName string
	}{
		{
			name:                  "Postgres DB",
			providerResourceName:  "env-app-name-db-1d9fb87",
			dbVersion:             "14.5",
			dbType:                AwsPostgres,
			expectedParameterName: "env-app-name-db-1d9fb87-14",
		},
		{
			name:                  "Aurora Postgres DB",
			providerResourceName:  "env-app-name-db-1d9fb87",
			dbVersion:             "13.3",
			dbType:                AwsAuroraPostgres,
			expectedParameterName: "env-app-name-db-1d9fb87-a-13",
		},
		{
			name:                  "Malformed version, should still extract major version",
			providerResourceName:  "env-app-name-db-1d9fb87",
			dbVersion:             "9",
			dbType:                AwsPostgres,
			expectedParameterName: "env-app-name-db-1d9fb87-9",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := getParameterGroupName(DatabaseSpec{ResourceName: test.providerResourceName, DBVersion: test.dbVersion, DbType: test.dbType})
			if result != test.expectedParameterName {
				t.Errorf("[%s] expected: %s, got: %s", test.name, test.expectedParameterName, result)
			}
		})
	}
}

func TestEnsureResource(t *testing.T) {
	RegisterTestingT(t)
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	ctx := context.Background()

	t.Run("should create resource when it doesn't exist", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		key := client.ObjectKey{
			Name:      "test-configmap",
			Namespace: "default",
		}

		createFunc := func() (*corev1.ConfigMap, error) {
			return &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "default",
				},
				Data: map[string]string{
					"test-key": "test-value",
				},
			}, nil
		}

		err := verifyOrCreateResource(ctx, cl, key, &corev1.ConfigMap{}, createFunc)
		Expect(err).To(BeNil())
		createdResource := &corev1.ConfigMap{}
		err = cl.Get(ctx, key, createdResource)
		Expect(err).To(BeNil())
		Expect(createdResource.Data["test-key"]).To(Equal("test-value"))
	})

	t.Run("should not create resource when it already exists", func(t *testing.T) {
		existingResource := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "existing-configmap",
				Namespace: "default",
			},
			Data: map[string]string{
				"existing-key": "existing-value",
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existingResource).
			Build()

		key := client.ObjectKey{
			Name:      "existing-configmap",
			Namespace: "default",
		}

		createFunc := func() (*corev1.ConfigMap, error) {
			return &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-configmap",
					Namespace: "default",
				},
				Data: map[string]string{
					"should-not-be-used": "new-value",
				},
			}, nil
		}
		err := verifyOrCreateResource(ctx, cl, key, &corev1.ConfigMap{}, createFunc)
		Expect(err).To(BeNil())
		existingAfter := &corev1.ConfigMap{}
		err = cl.Get(ctx, key, existingAfter)
		Expect(err).To(BeNil())
		Expect(existingAfter.Data).To(HaveKey("existing-key"))
		Expect(existingAfter.Data).NotTo(HaveKey("should-not-be-used"))
	})

	t.Run("should return error when Create fails", func(t *testing.T) {
		createInterceptor := func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			fmt.Println("Intercepting Create:", obj.GetName())
			return fmt.Errorf("test error")
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				Create: createInterceptor,
			}).Build()

		key := client.ObjectKey{
			Name:      "test-configmap",
			Namespace: "default",
		}

		createFunc := func() (*corev1.ConfigMap, error) {
			return &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "default",
				},
			}, nil
		}

		err := verifyOrCreateResource(ctx, cl, key, &corev1.ConfigMap{}, createFunc)
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("failed to create resource"))
	})
}
