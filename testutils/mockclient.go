package testutils

import (
	"context"
	"fmt"

	"github.com/aws/smithy-go/ptr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MockClient struct {
	client.Client
	Port string
}

func (m MockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	_ = ctx
	if (key.Namespace == "testNamespace" || key.Namespace == "testNamespaceWithDbIdentifierPrefix" || key.Namespace == "unitest") &&
		(key.Name == "sample-master-secret" || key.Name == "dbc-sample-connection" || key.Name == "dbc-sample-claim" || key.Name == "dbc-box-sample-claim" || key.Name == "test") {
		sec, ok := obj.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("can't assert type")
		}
		sec.Data = map[string][]byte{
			"password": []byte("masterpassword"),
		}
		return nil
	} else if key.Namespace == "schema-user-test" {
		sec, ok := obj.(*persistancev1.SchemaUserClaim)
		if !ok {
			return fmt.Errorf("can't assert type")
		}
		sec.Spec.Class = ptr.String("default")
		sec.Name = "schema1"
		sec.Spec.DBClaimName = "TestClaim"
		// sec.Spec.Database = &persistancev1.Database{
		// 	DSN:       "public://postgres:password@127.0.0.1:" + m.Port + "/postgres?sslmode=disable",
		// 	SecretRef: &persistancev1.SecretRef{Namespace: "unitest", Name: "test"},
		// }
		sec.Spec.Schemas = []persistancev1.SchemaUserType{
			{
				Name: "schema0",
			},
			{
				Name: "schema1",
				Users: []persistancev1.UserType{
					{
						UserName: "user1",
					},
				},
			},
			{
				Name: "schema2",
				Users: []persistancev1.UserType{
					{
						UserName: "user2_1",
					},
					{
						UserName: "user2_2",
					},
				},
			},
			{
				Name: "schema3",
				Users: []persistancev1.UserType{
					{
						UserName: "user3_1",
					},
					{
						UserName: "user3_2",
					},
					{
						UserName: "user3_3",
					},
				},
			},
		}

		return nil
	}
	return errors.NewNotFound(schema.GroupResource{Group: "core", Resource: "secret"}, key.Name)
}
func (m MockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	_ = ctx
	if (obj.GetNamespace() == "testNamespace") &&
		(obj.GetName() == "create-master-secret") {
		sec, ok := obj.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("can't assert type")
		}
		sec.Data = map[string][]byte{
			"password": []byte("masterpassword"),
		}
		return nil
	}
	return fmt.Errorf("can't create object")
}

type MockStatusWriter struct {
	client.StatusWriter
}

// func (m MockClient) Status() client.StatusWriter {
// 	return &MockStatusWriter{}
// }

func (m MockStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return nil
}

func (m MockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return nil
}
