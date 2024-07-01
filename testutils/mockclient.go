package testutils

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/smithy-go/ptr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MockClient struct {
	client.Client
	Port string
}

var responseUpdate interface{}

func (m MockClient) GetResponseUpdate() interface{} {
	return responseUpdate
}

func (m MockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	_ = ctx
	if (key.Namespace == "testNamespace" || key.Namespace == "testNamespaceWithDbIdentifierPrefix" || key.Namespace == "unitest" || key.Namespace == "schema-user-test") &&
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
		if key.Name == "TestClaim" { //DBClaim

			sec, ok := obj.(*persistancev1.DatabaseClaim)
			if !ok {
				return fmt.Errorf("can't assert type")
			}
			sec.Spec.Class = ptr.String("default")
			sec.Spec.SecretName = "sample-master-secret"
			sec.Name = "TestClaim"
			sec.Namespace = "schema-user-test"
			sec.Status = persistancev1.DatabaseClaimStatus{
				ActiveDB: persistancev1.Status{
					ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{
						DatabaseName: "postgres",
						Host:         "localhost",
						Port:         m.Port,
						Username:     "user_a",
						SSLMode:      "disable",
					},
				},
			}

		} else { //SchemaUserClaim
			sec, ok := obj.(*persistancev1.SchemaUserClaim)
			if !ok {
				return fmt.Errorf("can't assert type")
			}
			sec.Spec.Class = ptr.String("default")
			sec.Name = "schema1"
			sec.Spec.DBClaimName = "TestClaim"
			var time8DaysAgo = metav1.Time{Time: time.Now().Add(-8 * 24 * time.Hour)}
			sec.Spec.Schemas = []persistancev1.SchemaUserType{
				{
					Name: "schema0",
				},
				{
					Name: "schema1",
					Users: []persistancev1.UserType{
						{
							UserName:   "user1",
							Permission: persistancev1.Regular,
						},
					},
				},
				{
					Name: "schema2",
					Users: []persistancev1.UserType{
						{
							UserName:   "user2_1",
							Permission: persistancev1.Regular,
						},
						{
							UserName:   "user2_2",
							Permission: persistancev1.Admin,
						},
					},
				},
				{
					Name: "schema3",
					Users: []persistancev1.UserType{
						{
							UserName:   "user3_1",
							Permission: persistancev1.ReadOnly,
						},
						{
							UserName:   "user3_2",
							Permission: persistancev1.Regular,
						},
						{
							UserName:   "user3_3",
							Permission: persistancev1.Admin,
						},
						{
							UserName:   "user3_4",
							Permission: persistancev1.Admin,
						},
						{
							UserName:   "user3_5",
							Permission: persistancev1.Admin,
						},
					},
				},
				{
					Name: "public", //existing one
					Users: []persistancev1.UserType{
						{
							UserName:   "user4",
							Permission: persistancev1.Admin,
						},
					},
				},
				{
					Name: "schema4",
					Users: []persistancev1.UserType{
						{
							UserName:   "userAlreadyCreated",
							Permission: persistancev1.Admin,
						},
					},
				},
			}
			sec.Status.Schemas = []persistancev1.SchemaStatus{
				{
					Name:   "schema4",
					Status: "created",
					UsersStatus: []persistancev1.UserStatusType{
						{
							UserName:      "userAlreadyCreated_a",
							UserStatus:    "blablabla",
							UserUpdatedAt: &time8DaysAgo,
						},
					},
				},
			}
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

func (m MockClient) Status() client.StatusWriter {
	return &MockStatusWriter{}
}

func (m MockStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	responseUpdate = obj
	return nil
}

func (m MockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return nil
}
