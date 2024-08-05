package roleclaim

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/aws/smithy-go/ptr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type mockClient struct {
	client.Client
	// port is derived from the DSN
	dsn string
}

var responseUpdate interface{}

func (m *mockClient) GetResponseUpdate() interface{} {
	return responseUpdate
}

func (m *mockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return nil
}

func (m *mockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {

	parsedDSN, err := url.Parse(m.dsn)
	if err != nil {
		return err
	}
	port := parsedDSN.Port()

	_ = ctx
	var time8DaysAgo = metav1.Time{Time: time.Now().Add(-8 * 24 * time.Hour)}
	if (key.Namespace == "testNamespace" || key.Namespace == "testNamespaceWithDbIdentifierPrefix" || key.Namespace == "unitest" || key.Namespace == "schema-user-test") &&
		(key.Name == "sample-master-secret" || key.Name == "dbc-sample-connection" || key.Name == "dbc-sample-claim" || key.Name == "dbc-box-sample-claim" || key.Name == "test" || key.Name == "testclaim-1d9fb876") {
		sec, ok := obj.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("can't assert type")
		}
		sec.Data = map[string][]byte{
			"hostname": []byte("localhost"),
			"endpoint": []byte("localhost"), //specific to roleClaim
			"port":     []byte(port),
			"database": []byte("postgres"),
			"sslmode":  []byte("disable"),
			"password": []byte("masterpassword"),
			"username": []byte("mainUser"),
		}
		return nil
	} else if key.Namespace == "schema-user-test" {
		if key.Name == "testclaim" { //DBClaim

			sec, ok := obj.(*persistancev1.DatabaseClaim)
			if !ok {
				return fmt.Errorf("can't assert type")
			}
			sec.Spec.Class = ptr.String("default")
			sec.Spec.SecretName = "sample-master-secret"
			sec.Name = "testclaim"
			sec.Spec.DatabaseName = "postgres"
			sec.Namespace = "schema-user-test"
			sec.Status = persistancev1.DatabaseClaimStatus{
				ActiveDB: persistancev1.Status{
					ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{
						DatabaseName: "postgres",
						Host:         "localhost",
						Port:         port,
						Username:     "mainUser",
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
			sec.Spec.SourceDatabaseClaim = &persistancev1.SourceDatabaseClaim{Namespace: "schema-user-test", Name: "testclaim"}
			sec.Spec.SchemaRoleMap = make(map[string]persistancev1.RoleType)
			sec.Spec.SecretName = "sample-master-secret"
			sec.Name = "testclaim"
			sec.Namespace = "schema-user-test"

			sec.Spec.SchemaRoleMap["schema1"] = persistancev1.Regular
			sec.Spec.SchemaRoleMap["schema2"] = persistancev1.Admin
			sec.Spec.SchemaRoleMap["schema3"] = persistancev1.ReadOnly
			sec.Spec.SchemaRoleMap["public"] = persistancev1.Admin
			sec.Spec.SchemaRoleMap["schema4"] = persistancev1.Admin

		} else if key.Name == "schema-user-claim-2" || key.Name == "schema-user-claim-3" { //DBRoleClaim - EXISTING USER

			sec, ok := obj.(*persistancev1.DbRoleClaim)
			if !ok {
				return fmt.Errorf("can't assert type")
			}
			sec.Finalizers = append(sec.Finalizers, "dbroleclaims.persistance.atlas.infoblox.com/finalizer")
			sec.Spec.Class = ptr.String("default")
			sec.Spec.SourceDatabaseClaim = &persistancev1.SourceDatabaseClaim{Namespace: "schema-user-test", Name: "testclaim"}
			sec.Spec.SchemaRoleMap = make(map[string]persistancev1.RoleType)
			sec.Spec.SecretName = "sample-master-secret"
			sec.Name = "testclaim"
			sec.Namespace = "schema-user-test"

			if key.Name == "schema-user-claim-2" {
				sec.Spec.SchemaRoleMap["schema1"] = persistancev1.Regular
				sec.Spec.SchemaRoleMap["schema2"] = persistancev1.Admin
				sec.Spec.SchemaRoleMap["schema3"] = persistancev1.ReadOnly
				sec.Status.SchemasRolesUpdatedAt = &time8DaysAgo
			}
			if key.Name == "schema-user-claim-3" {
				sec.Spec.SchemaRoleMap["schema4"] = persistancev1.Admin
			}

			if key.Name == "schema-user-claim-2" {
				sec.Status.Username = "testclaim_user_a"
			}

		} else { //DBRoleClaim - invalid: schema without role

			sec, ok := obj.(*persistancev1.DbRoleClaim)
			if !ok {
				return fmt.Errorf("can't assert type")
			}
			sec.Spec.Class = ptr.String("default")
			sec.Name = "testclaim"
			sec.Spec.SourceDatabaseClaim = &persistancev1.SourceDatabaseClaim{Namespace: "schema-user-test", Name: "testclaim"}
			sec.Spec.SchemaRoleMap = make(map[string]persistancev1.RoleType, 6)

			sec.Spec.SchemaRoleMap["schema0"] = ""
		}

		return nil
	}
	return errors.NewNotFound(schema.GroupResource{Group: "core", Resource: "secret"}, key.Name)
}

func (m *mockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	_ = ctx
	if (obj.GetNamespace() == "testNamespace" || obj.GetNamespace() == "schema-user-test") &&
		(obj.GetName() == "create-master-secret" || obj.GetName() == "sample-master-secret") {
		sec, ok := obj.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("can't assert type")
		}
		sec.Data = map[string][]byte{
			"password": []byte("masterpassword"),
			"username": []byte("mainUser"),
		}
		return nil
	}
	return fmt.Errorf("can't create object")
}

func (m *mockClient) Status() client.StatusWriter {
	return &MockStatusWriter{}
}

type MockStatusWriter struct {
	client.StatusWriter
}

func (m MockStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	responseUpdate = obj
	return nil
}
