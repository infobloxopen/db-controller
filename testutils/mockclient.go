package testutils

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/smithy-go/ptr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	var time8DaysAgo = v1.Time{Time: time.Now().Add(-8 * 24 * time.Hour)}
	if (key.Namespace == "testNamespace" || key.Namespace == "testNamespaceWithDbIdentifierPrefix" || key.Namespace == "unitest" || key.Namespace == "schema-user-test") &&
		(key.Name == "sample-master-secret" || key.Name == "dbc-sample-connection" || key.Name == "dbc-sample-claim" || key.Name == "dbc-box-sample-claim" || key.Name == "test") {
		sec, ok := obj.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("can't assert type")
		}
		sec.Data = map[string][]byte{
			"password": []byte("masterpassword"),
			"username": []byte("user_a"),
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

		} else if key.Name == "schema-user-claim-1" { //DBRoleClaim

			sec, ok := obj.(*persistancev1.DbRoleClaim)
			if !ok {
				return fmt.Errorf("can't assert type")
			}
			sec.Spec.Class = ptr.String("default")
			sec.Name = "schema1"
			sec.Spec.SourceDatabaseClaim = &persistancev1.SourceDatabaseClaim{Namespace: "schema-user-test", Name: "TestClaim"}
			sec.Spec.SchemaRoleMap = make(map[string]persistancev1.RoleType)
			sec.Spec.SecretName = "sample-master-secret"
			sec.Name = "TestClaim"
			sec.Namespace = "schema-user-test"

			sec.Spec.UserName = "user1"
			sec.Spec.SchemaRoleMap["schema1"] = persistancev1.Regular
			sec.Spec.SchemaRoleMap["schema2"] = persistancev1.Admin
			sec.Spec.SchemaRoleMap["schema3"] = persistancev1.ReadOnly
			sec.Spec.SchemaRoleMap["public"] = persistancev1.Admin
			sec.Spec.SchemaRoleMap["schema4"] = persistancev1.Admin

		} else if key.Name == "schema-user-claim-2" { //DBRoleClaim - EXISTING USER

			sec, ok := obj.(*persistancev1.DbRoleClaim)
			if !ok {
				return fmt.Errorf("can't assert type")
			}
			sec.Spec.Class = ptr.String("default")
			sec.Name = "schema1"
			sec.Spec.SourceDatabaseClaim = &persistancev1.SourceDatabaseClaim{Namespace: "schema-user-test", Name: "TestClaim"}
			sec.Spec.SchemaRoleMap = make(map[string]persistancev1.RoleType)
			sec.Spec.SecretName = "sample-master-secret"
			sec.Name = "TestClaim"
			sec.Namespace = "schema-user-test"

			sec.Spec.UserName = "user2"
			sec.Spec.SchemaRoleMap["schema1"] = persistancev1.Regular
			sec.Spec.SchemaRoleMap["schema2"] = persistancev1.Admin
			sec.Spec.SchemaRoleMap["schema3"] = persistancev1.ReadOnly
			sec.Status.SchemasRolesUpdatedAt = &time8DaysAgo
			sec.Status.Username = "user2_a"

		} else { //DBRoleClaim - invalid: schema without role

			sec, ok := obj.(*persistancev1.DbRoleClaim)
			if !ok {
				return fmt.Errorf("can't assert type")
			}
			sec.Spec.Class = ptr.String("default")
			sec.Name = "schema1"
			sec.Spec.SourceDatabaseClaim = &persistancev1.SourceDatabaseClaim{Namespace: "schema-user-test", Name: "TestClaim"}
			sec.Spec.SchemaRoleMap = make(map[string]persistancev1.RoleType, 6)

			sec.Spec.UserName = "user1"
			sec.Spec.SchemaRoleMap["schema0"] = ""
		}

		return nil
	}
	return errors.NewNotFound(schema.GroupResource{Group: "core", Resource: "secret"}, key.Name)
}

func (m MockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	_ = ctx
	if (obj.GetNamespace() == "testNamespace" || obj.GetNamespace() == "schema-user-test") &&
		(obj.GetName() == "create-master-secret" || obj.GetName() == "sample-master-secret") {
		sec, ok := obj.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("can't assert type")
		}
		sec.Data = map[string][]byte{
			"password": []byte("masterpassword"),
			"username": []byte("user_a"),
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
