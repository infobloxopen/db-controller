package controllers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	"github.com/infobloxopen/db-controller/pkg/rdsauth"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var complexityEnabled = []byte(`
    passwordConfig:
      passwordComplexity: enabled
      minPasswordLength: "15"
      passwordRotationPeriod: "60"
`)

var complexityDisabled = []byte(`
    passwordConfig:
      passwordComplexity: disabled
      minPasswordLength: "15"
      passwordRotationPeriod: "60"
`)

var secretRef = []byte(`
    sample-connection:
      host: sample-master-host
      PasswordSecretRef: sample-master-secret
`)

var secretNoHostRef = []byte(`
    defaultReclaimPolicy: retain
    sample-connection:
      PasswordSecretRef: sample-master-secret
`)

var secretNoHostDeleteRef = []byte(`
    defaultReclaimPolicy: delete
    sample-connection:
      PasswordSecretRef: sample-master-secret
`)

var secretNoHosFragmentDeleteRef = []byte(`
    defaultReclaimPolicy: retain
    sample-connection:
      ReclaimPolicy: delete
      PasswordSecretRef: sample-master-secret
`)

var secretNoHostFragmentRetainRef = []byte(`
    defaultReclaimPolicy: delete
    sample-connection:
      ReclaimPolicy: retain
      PasswordSecretRef: sample-master-secret
`)

func TestDatabaseClaimReconcilerGeneratePassword(t *testing.T) {
	type reconciler struct {
		Client             client.Client
		Log                logr.Logger
		Scheme             *runtime.Scheme
		Config             *viper.Viper
		DbIdentifierPrefix string
	}
	tests := []struct {
		name    string
		rec     reconciler
		want    int
		wantErr bool
	}{
		{
			"Generate passwordComplexity enabled",
			reconciler{
				Config: NewConfig(complexityEnabled),
			},
			15,
			false,
		},
		{
			"Generate passwordComplexity disabled",
			reconciler{
				Config: NewConfig(complexityDisabled),
			},
			defaultPassLen,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client: tt.rec.Client,
				Log:    tt.rec.Log,
				Scheme: tt.rec.Scheme,
				Config: tt.rec.Config,
			}
			got, err := r.generatePassword()
			if (err != nil) != tt.wantErr {
				t.Errorf("generatePassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.want {
				t.Errorf("generatePassword() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func NewConfig(in []byte) *viper.Viper {
	c := viper.NewWithOptions(viper.KeyDelimiter("::"))
	c.SetConfigType("yaml")
	c.ReadConfig(bytes.NewBuffer(in))

	return c
}

type mockClient struct {
	client.Client
}

var opts = zap.Options{
	Development: true,
}

func (m mockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	_ = ctx
	if (key.Namespace == "testNamespace") &&
		(key.Name == "sample-master-secret" || key.Name == "dbc-sample-connection" ||
			key.Name == "dbc-sample-claim") {
		sec, ok := obj.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("can't assert type")
		}
		sec.Data = map[string][]byte{
			"password": []byte("masterpassword"),
		}
		return nil
	} else if (key.Namespace == "testNamespaceWithDbIdentifierPrefix") &&
		(key.Name == "dbc-box-sample-claim") {
		sec, ok := obj.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("can't assert type")
		}
		sec.Data = map[string][]byte{
			"password": []byte("masterpassword"),
		}
		return nil
	}
	return errors.NewNotFound(schema.GroupResource{Group: "core", Resource: "secret"}, key.Name)
}
func (m mockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
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

func TestDatabaseClaimReconcilerReadMasterPassword(t *testing.T) {
	type mockReconciler struct {
		Client             client.Client
		Log                logr.Logger
		Scheme             *runtime.Scheme
		Config             *viper.Viper
		DbIdentifierPrefix string
		Mode               ModeEnum
		Input              *input
	}
	type args struct {
		ctx         context.Context
		fragmentKey string
		namespace   string
		dbclaim     persistancev1.DatabaseClaim
	}
	tests := []struct {
		name       string
		reconciler mockReconciler
		args       args
		want       string
		wantErr    bool
	}{
		{
			"Get master password ok",
			mockReconciler{
				Client: &mockClient{},
				Config: NewConfig(secretRef),
			},
			args{
				fragmentKey: "sample-connection",
				namespace:   "testNamespace",
			},
			"masterpassword",
			false,
		},
		{
			"Get master password no secret name",
			mockReconciler{
				Client: &mockClient{},
				Config: NewConfig(secretRef),
			},
			args{
				fragmentKey: "",
				namespace:   "testNamespace",
			},
			"",
			true,
		},
		// {
		// 	"Get dynamic database master password no fragment, with env Name",
		// 	mockReconciler{
		// 		Client: &mockClient{},
		// 		Config: NewConfig(secretRef),
		// 	},
		// 	args{
		// 		fragmentKey: "",
		// 		namespace:   "testNamespaceWithDbIdentifierPrefix",
		// 		dbclaim:     persistancev1.DatabaseClaim{ObjectMeta: metav1.ObjectMeta{Name: "sample-claim"}},
		// 	},
		// 	"masterpassword",
		// 	false,
		// },

		{
			"Get dynamic database master password no fragment host",
			mockReconciler{
				Client: &mockClient{},
				Config: NewConfig(secretNoHostRef),
			},
			args{
				fragmentKey: "sample-connection",
				namespace:   "testNamespace",
			},
			"masterpassword",
			false,
		},
		{
			"Get master password secret not found",
			mockReconciler{
				Client: &mockClient{},
				Config: NewConfig(secretRef),
			},
			args{
				fragmentKey: "secretNameNotExists",
				namespace:   "testNamespace",
			},
			"",
			true,
		},
	}
	for _, tt := range tests {
		t.Setenv("SERVICE_NAMESPACE", tt.args.namespace)
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client:             tt.reconciler.Client,
				Log:                tt.reconciler.Log,
				Scheme:             tt.reconciler.Scheme,
				Config:             tt.reconciler.Config,
				DbIdentifierPrefix: tt.reconciler.DbIdentifierPrefix,
				Input:              &input{FragmentKey: tt.args.fragmentKey},
			}
			// got, err := r.readMasterPassword(tt.args.ctx, tt.args.fragmentKey, &tt.args.dbclaim, tt.args.namespace)
			got, err := r.readMasterPassword(tt.args.ctx, &tt.args.dbclaim)
			if (err != nil) != tt.wantErr {
				t.Errorf("readMasterPassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("readMasterPassword() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetReclaimPolicy(t *testing.T) {
	type mockReconciler struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
		Config *viper.Viper
	}
	type args struct {
		fragmentKey string
	}
	tests := []struct {
		name       string
		reconciler mockReconciler
		args       args
		want       string
	}{
		{
			"Get retain with empty fragment key",
			mockReconciler{
				Client: &mockClient{},
				Config: NewConfig(secretNoHostRef),
			},
			args{
				fragmentKey: "sample-connection",
			},
			"retain",
		},
		{
			"Get delete with empty fragment key",
			mockReconciler{
				Client: &mockClient{},
				Config: NewConfig(secretNoHostDeleteRef),
			},
			args{
				fragmentKey: "sample-connection",
			},
			"delete",
		},
		{
			"Get delete with fragment key",
			mockReconciler{
				Client: &mockClient{},
				Config: NewConfig(secretNoHosFragmentDeleteRef),
			},
			args{
				fragmentKey: "sample-connection",
			},
			"delete",
		},
		{
			"Get retain with fragment key",
			mockReconciler{
				Client: &mockClient{},
				Config: NewConfig(secretNoHostFragmentRetainRef),
			},
			args{
				fragmentKey: "sample-connection",
			},
			"retain",
		},
		{
			"Get delete from defaultReclaimPolicy with no fragment key",
			mockReconciler{
				Client: &mockClient{},
				Config: NewConfig(secretNoHostFragmentRetainRef),
			},
			args{
				fragmentKey: "",
			},
			"delete",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client: tt.reconciler.Client,
				Log:    tt.reconciler.Log,
				Scheme: tt.reconciler.Scheme,
				Config: tt.reconciler.Config,
			}
			got := r.getReclaimPolicy(tt.args.fragmentKey)
			if got != tt.want {
				t.Errorf("getReclaimPolicy() got = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDatabaseClaimReconcilerGetSecretRef(t *testing.T) {
	type mockReconciler struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
		Config *viper.Viper
	}
	type args struct {
		fragmentKey string
	}
	tests := []struct {
		name       string
		reconciler mockReconciler
		args       args
		want       string
	}{
		{
			"Get master secret reference",
			mockReconciler{
				Config: NewConfig(secretRef),
			},
			args{fragmentKey: "sample-connection"},
			"sample-master-secret",
		},
		{
			"Get master secret reference connection does not exist",
			mockReconciler{
				Config: NewConfig(secretRef),
			},
			args{fragmentKey: "sample-connection111"},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client: tt.reconciler.Client,
				Log:    tt.reconciler.Log,
				Scheme: tt.reconciler.Scheme,
				Config: tt.reconciler.Config,
			}
			if got := r.getSecretRef(tt.args.fragmentKey); got != tt.want {
				t.Errorf("getSecretRef() = %v, want %v", got, tt.want)
			}
		})
	}
}

var testConfig = []byte(`
    defaultMasterPort: 5432
    defaultMasterUsername: root
    defaultSslMode: require
    sample-connection:
      Username: postgres
      Host: db-controller-postgresql
      Port: 5432
      sslMode: false
      PasswordSecretRef: sample-master-secret
    namespace:
      deniedList: authz entit*
      allowedList: test* identity
`)

// TODO - Write additional tests for dynamic host allocation
func TestDatabaseClaimReconcilerGetConnectionParams(t *testing.T) {
	type mockReconciler struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
		Config *viper.Viper
	}
	type args struct {
		fragmentKey string
		dbClaim     *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name       string
		reconciler mockReconciler
		args       []args
		want       []string
	}{
		{
			"Get master connection params Host Port Username",
			mockReconciler{
				Config: NewConfig(testConfig),
			},
			[]args{
				{
					fragmentKey: "sample-connection",
					dbClaim: &persistancev1.DatabaseClaim{
						Spec: persistancev1.DatabaseClaimSpec{
							Host: "",
						},
					},
				},
				{
					fragmentKey: "sample-connection",
					dbClaim: &persistancev1.DatabaseClaim{
						Spec: persistancev1.DatabaseClaimSpec{
							Host: "overridden-host",
						},
					},
				},
				{
					fragmentKey: "sample-connection",
					dbClaim: &persistancev1.DatabaseClaim{
						Spec: persistancev1.DatabaseClaimSpec{
							Port: "",
						},
					},
				},
				{
					fragmentKey: "sample-connection",
					dbClaim: &persistancev1.DatabaseClaim{
						Spec: persistancev1.DatabaseClaimSpec{
							Port: "1234",
						},
					},
				},
				{
					fragmentKey: "sample-connection",
					dbClaim: &persistancev1.DatabaseClaim{
						Spec: persistancev1.DatabaseClaimSpec{},
					}},
				{
					fragmentKey: "",
					dbClaim: &persistancev1.DatabaseClaim{
						Spec: persistancev1.DatabaseClaimSpec{},
					},
				},
				{
					fragmentKey: "",
					dbClaim: &persistancev1.DatabaseClaim{
						Spec: persistancev1.DatabaseClaimSpec{},
					},
				},
				{
					fragmentKey: "",
					dbClaim: &persistancev1.DatabaseClaim{
						Spec: persistancev1.DatabaseClaimSpec{},
					},
				},
			},
			[]string{
				"db-controller-postgresql",
				"overridden-host",
				"5432",
				"1234",
				"root",
				"root",
				"5432",
				"require",
			},
		},
	}
	// TODO - Make this more DRY and put under a for loop.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client: tt.reconciler.Client,
				Log:    tt.reconciler.Log,
				Scheme: tt.reconciler.Scheme,
				Config: tt.reconciler.Config,
				Input:  &input{FragmentKey: tt.args[0].fragmentKey},
			}
			t.Log("getMasterHost() Host from testConfig")
			if got := r.getMasterHost(tt.args[0].dbClaim); got != tt.want[0] {
				t.Errorf("getMasterHost() = %v, want %v", got, tt.want[0])
			}
			t.Log("getMasterHost() Host from testConfig PASS")

			t.Log("getMasterHost() Host overridden by DB claim")
			if got := r.getMasterHost(tt.args[1].dbClaim); got != tt.want[1] {
				t.Errorf("getMasterPort() = %v, want %v", got, tt.want[1])
			}
			t.Log("getMasterPort() Host overridden by DB claim PASS")

			t.Log("getMasterHost() Port from testConfig")
			if got := r.getMasterPort(tt.args[2].dbClaim); got != tt.want[2] {
				t.Errorf("getMasterPort() = %v, want %v", got, tt.want[2])
			}
			t.Log("getMasterPort() Port from testConfig PASS")

			t.Log("getMasterHost() Port overridden by DB claim")
			if got := r.getMasterPort(tt.args[3].dbClaim); got != tt.want[3] {
				t.Errorf("getMasterPort() = %v, want %v", got, tt.want[3])
			}
			t.Log("getMasterPort() Port overridden by DB claim PASS")

			if got := r.getMasterUser(tt.args[4].dbClaim); got != tt.want[4] {
				t.Errorf("getMasterUser() = %v, want %v", got, tt.want[4])
			}
			t.Log("getMasterUser() PASS")

			r = &DatabaseClaimReconciler{
				Client: tt.reconciler.Client,
				Log:    tt.reconciler.Log,
				Scheme: tt.reconciler.Scheme,
				Config: tt.reconciler.Config,
				Input:  &input{FragmentKey: tt.args[5].fragmentKey},
			}

			if got := r.getMasterUser(tt.args[5].dbClaim); got != tt.want[5] {
				t.Errorf("getMasterUser() = %v, want %v", got, tt.want[5])
			}
			t.Log("getMasterUser() Username from default value in config PASS")

			if got := r.getMasterPort(tt.args[6].dbClaim); got != tt.want[6] {
				t.Errorf("getMasterPort() = %v, want %v", got, tt.want[6])
			}
			t.Log("getMasterPort() Port from default value in config PASS")

			if got := r.getSSLMode(tt.args[7].dbClaim); got != tt.want[7] {
				t.Errorf("getSSLMode() = %v, want %v", got, tt.want[7])
			}

		})
	}
}

var sslModeDisabled = []byte(`
    sample-connection:
      host: some-host
      sslMode: disable
`)

var sslModeEnabled = []byte(`
    sample-connection:
      host: some-host
      sslMode: require
`)

var sslModeDefault = []byte(`
    defaultSslMode: require
    sample-connection:
      sslMode: disable
`)

func TestDatabaseClaimReconcilerGetSSLMode(t *testing.T) {
	type mockReconciler struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
		Config *viper.Viper
	}
	type args struct {
		fragmentKey string
		dbClaim     *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name       string
		reconciler mockReconciler
		args       args
		want       string
	}{
		{
			"Get master connection SSL mode",
			mockReconciler{
				Config: NewConfig(sslModeDisabled),
			},
			args{
				fragmentKey: "sample-connection",
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{},
				},
			},
			"disable",
		},
		{
			"Get master connection params Host, Port, user name",
			mockReconciler{
				Config: NewConfig(sslModeEnabled),
			},
			args{
				fragmentKey: "sample-connection",
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{},
				},
			},
			"require",
		},
		{
			"Get SslMode from default Config",
			mockReconciler{
				Config: NewConfig(sslModeDefault),
			},
			args{
				fragmentKey: "sample-connection",
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{},
				},
			},
			"disable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client: tt.reconciler.Client,
				Log:    tt.reconciler.Log,
				Scheme: tt.reconciler.Scheme,
				Config: tt.reconciler.Config,
				Input:  &input{FragmentKey: tt.args.fragmentKey},
			}
			if got := r.getSSLMode(tt.args.dbClaim); got != tt.want {
				t.Errorf("getSSLMode() = %v, want %v", got, tt.want)
			}
			t.Log("getSSLMode() PASS")
		})
	}
}

var multiConfig = []byte(`
  authSource: secret
  # if aws authorization is used iam role must be provided
  #iamRole: rds-role
  dbMultiAZEnabled: false
  region: us-east-1
  pgTemp: "/pg-temp/"
  vpcSecurityGroupIDRefs:
  dbSubnetGroupNameRef:
  dynamicHostWaitTimeMin: 1
  defaultShape: db.t4g.medium
  defaultMinStorageGB: 20
  defaultEngine: postgres
  defaultEngineVersion: 12.11
  defaultMasterPort: 5432
  defaultSslMode: require
  defaultMasterUsername: root
  defaultReclaimPolicy: delete
  # For Production this should be false and if SnapShot is not taken it will not be deleted
  defaultSkipFinalSnapshotBeforeDeletion: true
  defaultPubliclyAccessible: false
  defaultDeletionPolicy: orphan
  providerConfig: default
  defaultBackupPolicyValue: Bronze
  passwordConfig:
    passwordComplexity: enabled
    minPasswordLength: 15
    passwordRotationPeriod: 60
  sample-connection:
    username: postgres
    host: localhost
    port: 5432
    sslMode: disable
    passwordSecretRef: postgres-postgresql
    passwordSecretKey: postgresql-password
  # host omitted, allocates database dynamically
  dynamic-connection:
    username: root
    port: 5432
    sslMode: require
    passwordSecretRef: dynamic-connection-secret
    shape: db.t4g.medium
    minStorageGB: 20
    engine: postgres
    engineVersion: 12.8
    reclaimPolicy: delete
  another:
    username: root
    host: some.other.service
    port: 5412
    sslMode: require
    passwordSecretRef: another-connection-secret
`)

func TestDatabaseClaimReconcilerMatchInstanceLabel(t *testing.T) {
	type mockReconciler struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
		Config *viper.Viper
	}
	type args struct {
		dbClaim *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name       string
		reconciler mockReconciler
		args       args
		want       string
		wantErr    bool
	}{
		{
			"Get fragment key",
			mockReconciler{
				Config: NewConfig(multiConfig),
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						InstanceLabel: "sample-connection",
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}},
						NewDB:    persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}},
					},
				},
			},
			"sample-connection",
			false,
		},
		{
			"Get partial match fragment key",
			mockReconciler{
				Config: NewConfig(multiConfig),
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						InstanceLabel: "another-connection",
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}},
						NewDB:    persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}},
					},
				},
			},
			"another",
			false,
		},

		{
			"No key match",
			mockReconciler{
				Config: NewConfig(multiConfig),
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						InstanceLabel: "blabla",
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}},
						NewDB:    persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}},
					},
				},
			},
			"",
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client: tt.reconciler.Client,
				Log:    tt.reconciler.Log,
				Scheme: tt.reconciler.Scheme,
				Config: tt.reconciler.Config,
			}
			got, err := r.matchInstanceLabel(tt.args.dbClaim)
			if (err != nil) != tt.wantErr {
				t.Errorf("matchInstanceLabel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("matchInstanceLabel() got = %v, want %v", got, tt.want)
			}
		})
	}
}

var passwordLength = []byte(`
    passwordConfig:
      minPasswordLength: "15"
`)

func TestDatabaseClaimReconcilerGetMinPasswordLength(t *testing.T) {
	type mockReconciler struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
		Config *viper.Viper
	}
	tests := []struct {
		name       string
		reconciler mockReconciler
		want       int
	}{
		{
			"Get password length",
			mockReconciler{
				Config: NewConfig(passwordLength),
			},
			15,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client: tt.reconciler.Client,
				Log:    tt.reconciler.Log,
				Scheme: tt.reconciler.Scheme,
				Config: tt.reconciler.Config,
			}
			if got := r.getMinPasswordLength(); got != tt.want {
				t.Errorf("getMinPasswordLength() = %v, want %v", got, tt.want)
			}
		})
	}
}

var passwordRotation = []byte(`
    passwordConfig:
      passwordRotationPeriod: "75"
`)

var passwordRotationLess60 = []byte(`
    passwordConfig:
      passwordRotationPeriod: "50"
`)

// var passwordRotationGt1440 = []byte(`
//     passwordConfig:
//       passwordRotationPeriod: "2000"
// `)

func TestDatabaseClaimReconcilerGetPasswordRotationTime(t *testing.T) {
	type mockReconciler struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
		Config *viper.Viper
	}
	tests := []struct {
		name       string
		reconciler mockReconciler
		want       time.Duration
	}{
		{
			"Get password rotation time",
			mockReconciler{
				Config: NewConfig(passwordRotation),
				Log:    zap.New(zap.UseDevMode(true)),
			},
			75 * time.Minute,
		},
		{
			"Get password rotation time less 60 min",
			mockReconciler{
				Config: NewConfig(passwordRotationLess60),
				Log:    zap.New(zap.UseDevMode(true)),
			},
			defaultRotationTime * time.Minute,
		},
		{
			"Get password rotation time greater 1440 min",
			mockReconciler{
				Config: NewConfig(passwordRotationLess60),
				Log:    zap.New(zap.UseDevMode(true)),
			},
			defaultRotationTime * time.Minute,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client: tt.reconciler.Client,
				Log:    tt.reconciler.Log,
				Scheme: tt.reconciler.Scheme,
				Config: tt.reconciler.Config,
			}
			if got := r.getPasswordRotationTime(); got != tt.want {
				t.Errorf("getPasswordRotationTime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetDBName(t *testing.T) {
	type args struct {
		dbClaim *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			"DB is not overridden",
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						DBNameOverride: "",
						DatabaseName:   "db_name",
					},
				},
			},
			"db_name",
		},
		{
			"DB is overridden",
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						DBNameOverride: "overridden_db_name",
						DatabaseName:   "db_name",
					},
				},
			},
			"overridden_db_name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetDBName(tt.args.dbClaim); got != tt.want {
				t.Errorf("GetDBName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getServiceNamespace(t *testing.T) {
	os.Setenv(serviceNamespaceEnvVar, "service-namespace")
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{
			"service namespace exists",
			"service-namespace",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getServiceNamespace()
			if (err != nil) != tt.wantErr {
				t.Errorf("getServiceNamespace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getServiceNamespace() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDatabaseClaimReconciler_isClassPermitted(t *testing.T) {
	type mockReconciler struct {
		Config *viper.Viper
		Class  string
	}
	type args struct {
		claimClass string
	}
	tests := []struct {
		name       string
		reconciler mockReconciler
		args       args
		want       bool
	}{
		{
			"permitted_ok",
			mockReconciler{
				Config: NewConfig(testConfig),
				Class:  "default",
			},
			args{
				claimClass: "default",
			},
			true,
		},
		{
			"denied",
			mockReconciler{
				Config: NewConfig(testConfig),
				Class:  "default",
			},
			args{
				claimClass: "danny",
			},
			false,
		},
		{
			"ok empty classes",
			mockReconciler{
				Config: NewConfig(testConfig),
				Class:  "",
			},
			args{
				claimClass: "",
			},
			true,
		},
		{
			"permitted with empty class",
			mockReconciler{
				Config: NewConfig(testConfig),
				Class:  "default",
			},
			args{
				claimClass: "",
			},
			true,
		},
		{
			"denied  - non default",
			mockReconciler{
				Config: NewConfig(testConfig),
				Class:  "bjeevan",
			},
			args{
				claimClass: "",
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Config: tt.reconciler.Config,
				Class:  tt.reconciler.Class,
			}
			got := r.isClassPermitted(tt.args.claimClass)
			if got != tt.want {
				t.Errorf("DatabaseClaimReconciler.isClassPermitted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDatabaseClaimReconciler_getDynamicHostName(t *testing.T) {
	type fields struct {
		Client             client.Client
		Log                logr.Logger
		Scheme             *runtime.Scheme
		Config             *viper.Viper
		MasterAuth         *rdsauth.MasterAuth
		DbIdentifierPrefix string
		Mode               ModeEnum
		Input              *input
	}
	type args struct {
		dbClaim *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   string
	}{
		{
			"OK",
			fields{
				Config:             NewConfig(multiConfig),
				DbIdentifierPrefix: "boxing-x",
				Input: &input{FragmentKey: "",
					HostParams: hostparams.HostParams{Engine: "postgres",
						EngineVersion: "12.11",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20}},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name"},
					Spec: persistancev1.DatabaseClaimSpec{
						AppID:        "identity",
						DatabaseName: "identity",
					},
				},
			},
			"boxing-x-identity-dbclaim-name-d391a72a",
		},
		{
			"OK",
			fields{
				Config:             NewConfig(multiConfig),
				DbIdentifierPrefix: "boxing-x",
				Input: &input{FragmentKey: "athena",
					HostParams: hostparams.HostParams{Engine: "aurora-postgresql",
						EngineVersion: "12.11",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20}},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name"},
					Spec: persistancev1.DatabaseClaimSpec{
						AppID:        "identity",
						DatabaseName: "identity",
					},
				},
			},
			"boxing-x-athena-362c4cd6",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client:             tt.fields.Client,
				Log:                tt.fields.Log,
				Scheme:             tt.fields.Scheme,
				Config:             tt.fields.Config,
				MasterAuth:         tt.fields.MasterAuth,
				DbIdentifierPrefix: tt.fields.DbIdentifierPrefix,
				Mode:               tt.fields.Mode,
				Input:              tt.fields.Input,
			}
			if got := r.getDynamicHostName(tt.args.dbClaim); got != tt.want {
				t.Errorf("DatabaseClaimReconciler.getDynamicHostName() = %v, want %v", got, tt.want)
			}
		})
	}
}
func TestDatabaseClaimReconciler_setReqInfo(t *testing.T) {
	opts := zap.Options{
		Development: true,
	}
	flse := false
	type fields struct {
		Client             client.Client
		Log                logr.Logger
		Scheme             *runtime.Scheme
		Config             *viper.Viper
		MasterAuth         *rdsauth.MasterAuth
		DbIdentifierPrefix string
		Mode               ModeEnum
		Input              *input
	}
	type args struct {
		dbClaim *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   error
	}{
		{
			"OK",
			fields{
				Config:             NewConfig(multiConfig),
				Log:                zap.New(zap.UseFlagOptions(&opts)),
				DbIdentifierPrefix: "boxing-x",
				Input: &input{FragmentKey: "",
					HostParams: hostparams.HostParams{Engine: "postgres",
						EngineVersion: "12.11",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20}},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "44characterlengthname23456789012345678901234"},
					Spec: persistancev1.DatabaseClaimSpec{
						AppID:                 "identity",
						DatabaseName:          "identity",
						EnableReplicationRole: &flse},
				},
			},
			nil,
		},
		{
			"Dbname too long",
			fields{
				Config:             NewConfig(multiConfig),
				Log:                zap.New(zap.UseFlagOptions(&opts)),
				DbIdentifierPrefix: "boxing-x",
				Input: &input{FragmentKey: "",
					HostParams: hostparams.HostParams{Engine: "postgres",
						EngineVersion: "12.11",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20}},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "nametooooooooooooooooooloooooooooooooooooooong"},
					Spec: persistancev1.DatabaseClaimSpec{
						AppID:                 "identity",
						DatabaseName:          "identity",
						EnableReplicationRole: &flse},
				},
			},
			ErrMaxNameLen,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client:             tt.fields.Client,
				Log:                tt.fields.Log,
				Scheme:             tt.fields.Scheme,
				Config:             tt.fields.Config,
				MasterAuth:         tt.fields.MasterAuth,
				DbIdentifierPrefix: tt.fields.DbIdentifierPrefix,
				Mode:               tt.fields.Mode,
				Input:              tt.fields.Input,
			}
			if got := r.setReqInfo(tt.args.dbClaim); got != tt.want {
				t.Errorf("DatabaseClaimReconciler.setReqInfo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDatabaseClaimReconciler_getMode(t *testing.T) {
	tru := true
	flse := false

	type fields struct {
		Client             client.Client
		Log                logr.Logger
		Scheme             *runtime.Scheme
		Config             *viper.Viper
		MasterAuth         *rdsauth.MasterAuth
		DbIdentifierPrefix string
		Mode               ModeEnum
		Input              *input
		Class              string
	}

	type args struct {
		dbClaim *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   ModeEnum
	}{
		{
			"useExistingWithNoSource",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					SharedDBHost: false,
				},
			},

			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &tru,
						SourceDataFrom:    nil,
					},
				},
			},
			M_NotSupported,
		},
		{
			"useExistingFalse-WithNoSource-WithStatusUsingExistingDB",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					SharedDBHost: false,
				},
			},

			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
						SourceDataFrom:    nil,
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{DbState: "using-existing-db",
							SourceDataFrom: &persistancev1.SourceDataFrom{
								Type: persistancev1.SourceDataType("database"),
								Database: &persistancev1.Database{DSN: "postgres://r@h:5432/pub?sslmode=require",
									SecretRef: &persistancev1.SecretRef{Namespace: "unitest", Name: "test"},
								},
							},
						},
					},
				},
			},
			M_MigrateExistingToNewDB,
		},
		{
			"useExistingFalse-WithNoSource-WithStatusUsingExistingDB-noSourceDataFromInStatus",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					SharedDBHost: false,
				},
			},

			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
						SourceDataFrom:    nil,
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{DbState: "using-existing-db",
							SourceDataFrom: nil,
						},
					},
				},
			},
			M_NotSupported,
		},

		{
			"useExisting_DatabaseType",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					SharedDBHost: false,
				},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &tru,
						SourceDataFrom: &persistancev1.SourceDataFrom{
							Type: persistancev1.SourceDataType("database"),
							Database: &persistancev1.Database{DSN: "postgres://r@h:5432/pub?sslmode=require",
								SecretRef: &persistancev1.SecretRef{Namespace: "unitest", Name: "test"},
							},
						},
					},
				},
			},
			M_UseExistingDB,
		},
		{
			"useExisting_S3Type",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					SharedDBHost: false,
				},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &tru,
						SourceDataFrom: &persistancev1.SourceDataFrom{
							Type: persistancev1.SourceDataType("s3"),
						},
					},
				},
			},
			M_NotSupported,
		},
		{
			"migrateExistingToNewDB",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					HostParams: hostparams.HostParams{
						Engine:        "postgres",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20,
						EngineVersion: "12.11",
					},
					SharedDBHost: false,
				},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
						SourceDataFrom: &persistancev1.SourceDataFrom{
							Type: persistancev1.SourceDataType("database"),
							Database: &persistancev1.Database{DSN: "postgres://r@h:5432/pub?sslmode=require",
								SecretRef: &persistancev1.SecretRef{Namespace: "unitest", Name: "test"},
							},
						},
						Type:         "postgres",
						Shape:        "db.t4g.medium",
						MinStorageGB: 20,
						DBVersion:    "12.11",
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{DbState: "using-existing-db"},
					},
				},
			},
			M_MigrateExistingToNewDB,
		},
		{
			"MigrationOfExistingDBVtoNewDBInProgress",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					HostParams: hostparams.HostParams{
						Engine:        "postgres",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20,
						EngineVersion: "12.11",
					},
					SharedDBHost: false},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
						SourceDataFrom: &persistancev1.SourceDataFrom{
							Type: persistancev1.SourceDataType("database"),
							Database: &persistancev1.Database{DSN: "postgres://r@h:5432/pub?sslmode=require",
								SecretRef: &persistancev1.SecretRef{Namespace: "unitest", Name: "test"},
							},
						},
						Type:         "postgres",
						Shape:        "db.t4g.medium",
						MinStorageGB: 20,
						DBVersion:    "12.11",
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB:       persistancev1.Status{DbState: "using-existing-db"},
						MigrationState: "something in progress",
					},
				},
			},
			M_MigrationInProgress,
		},
		{
			"sourcedatapresent_newNewDb",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{HostParams: hostparams.HostParams{
					Engine:        "postgres",
					Shape:         "db.t4g.medium",
					MinStorageGB:  20,
					EngineVersion: "12.11"},
					SharedDBHost: false},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},
					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
						SourceDataFrom: &persistancev1.SourceDataFrom{
							Type: persistancev1.SourceDataType("database"),
							Database: &persistancev1.Database{
								DSN:       "postgres://r@h:5432/pub?sslmode=require",
								SecretRef: &persistancev1.SecretRef{Namespace: "unitest", Name: "test"},
							},
						},
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							DbState:      persistancev1.Ready,
							Type:         "postgres",
							DBVersion:    "12.11",
							Shape:        "db.t4g.medium",
							MinStorageGB: 20,
						},
					},
				},
			},
			M_UseNewDB,
		},
		{
			"sourcedatapresent_upgradeDB",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					HostParams: hostparams.HostParams{
						Engine:        "aurora-postgres",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20,
						EngineVersion: "12.11",
					},
					SharedDBHost: false,
				},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},
					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
						SourceDataFrom: &persistancev1.SourceDataFrom{
							Type: persistancev1.SourceDataType("database"),
							Database: &persistancev1.Database{
								DSN:       "postgres://r@h:5432/pub?sslmode=require",
								SecretRef: &persistancev1.SecretRef{Namespace: "unitest", Name: "test"},
							},
						},
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							DbState:      persistancev1.Ready,
							Type:         "postgres",
							DBVersion:    "12.11",
							Shape:        "db.t4g.medium",
							MinStorageGB: 20,
						},
					},
				},
			},
			M_InitiateDBUpgrade,
		},
		{
			"sourceNotpresent_UpgradeDBInProgress",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					HostParams: hostparams.HostParams{
						Engine:        "aurora-postgres",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20,
						EngineVersion: "12.11",
					},
				},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},
					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
						SourceDataFrom: &persistancev1.SourceDataFrom{
							Type: persistancev1.SourceDataType("database"),
							Database: &persistancev1.Database{
								DSN:       "postgres://r@h:5432/pub?sslmode=require",
								SecretRef: &persistancev1.SecretRef{Namespace: "unitest", Name: "test"},
							},
						},
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							DbState:      persistancev1.Ready,
							Type:         "postgres",
							DBVersion:    "12.11",
							Shape:        "db.t4g.medium",
							MinStorageGB: 20,
						},
						NewDB: persistancev1.Status{
							DbState: "in-progress",
						},
						MigrationState: "something in progress",
					},
				},
			},
			M_UpgradeDBInProgress,
		},
		{
			"useSharedDBHost",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{HostParams: hostparams.HostParams{
					Engine:        "aurora-postgres",
					Shape:         "db.t4g.medium",
					MinStorageGB:  20,
					EngineVersion: "12.11",
				},
				},
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
						InstanceLabel:     "testLabel",
					},
				},
			},
			M_UseNewDB,
		},
		{
			"useSharedDBHost-withUpgradeRequest",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					HostParams: hostparams.HostParams{
						Engine:        "aurora-postgres",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20,
						EngineVersion: "12.11",
					},
					SharedDBHost: true,
				},
			},

			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							DbState:      persistancev1.Ready,
							Type:         "aurora-postgres",
							DBVersion:    "13.11",
							Shape:        "db.t4g.medium",
							MinStorageGB: 20,
						},
					},
				},
			},
			M_UseNewDB,
		},
		{
			"postMigrationActions-positive",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					HostParams: hostparams.HostParams{
						Engine:        "aurora-postgres",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20,
						EngineVersion: "12.11",
					},
					SharedDBHost: false,
				},
			},

			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							DbState: persistancev1.Ready,
						},
						OldDB: persistancev1.Status{
							DbState:        persistancev1.PostMigrationInProgress,
							Type:           "aurora-postgres",
							DBVersion:      "13.11",
							Shape:          "db.t4g.medium",
							MinStorageGB:   20,
							ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{},
						},
					},
				},
			},
			M_PostMigrationInProgress,
		},
		{
			"postMigrationActions-negative-without-connectionInfo-in-oldDB",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					HostParams: hostparams.HostParams{
						Engine:        "aurora-postgres",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20,
						EngineVersion: "12.11",
					},
					SharedDBHost: false,
				},
			},

			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							DbState: persistancev1.Ready,
						},
						OldDB: persistancev1.Status{
							DbState:      persistancev1.PostMigrationInProgress,
							Type:         "aurora-postgres",
							DBVersion:    "13.11",
							Shape:        "db.t4g.medium",
							MinStorageGB: 20,
						},
					},
				},
			},
			M_NotSupported,
		},
		{
			"postMigrationActions-negative-wit-userExistingSource",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					HostParams: hostparams.HostParams{
						Engine:        "aurora-postgres",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20,
						EngineVersion: "12.11",
					},
					SharedDBHost: false,
				},
			},

			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &tru,
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							DbState: persistancev1.Ready,
						},
						OldDB: persistancev1.Status{
							DbState:      persistancev1.PostMigrationInProgress,
							Type:         "aurora-postgres",
							DBVersion:    "13.11",
							Shape:        "db.t4g.medium",
							MinStorageGB: 20,
						},
					},
				},
			},
			M_NotSupported,
		},
		{
			"postMigrationActions-negative-with-sourceData",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					HostParams: hostparams.HostParams{
						Engine:        "aurora-postgres",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20,
						EngineVersion: "12.11",
					},
					SharedDBHost: false,
				},
			},

			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
						SourceDataFrom:    &persistancev1.SourceDataFrom{},
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							DbState: persistancev1.Ready,
						},
						OldDB: persistancev1.Status{
							DbState:      persistancev1.PostMigrationInProgress,
							Type:         "aurora-postgres",
							DBVersion:    "13.11",
							Shape:        "db.t4g.medium",
							MinStorageGB: 20,
						},
					},
				},
			},
			M_NotSupported,
		},
		{
			"postMigrationActions-negative-with-sharedDB",
			fields{
				Log: zap.New(zap.UseFlagOptions(&opts)),
				Input: &input{
					HostParams: hostparams.HostParams{
						Engine:        "aurora-postgres",
						Shape:         "db.t4g.medium",
						MinStorageGB:  20,
						EngineVersion: "12.11",
					},
					SharedDBHost: true,
				},
			},

			args{
				dbClaim: &persistancev1.DatabaseClaim{
					ObjectMeta: v1.ObjectMeta{Name: "identity-dbclaim-name",
						Namespace: "unitest"},

					Spec: persistancev1.DatabaseClaimSpec{
						UseExistingSource: &flse,
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							DbState: persistancev1.Ready,
						},
						OldDB: persistancev1.Status{
							DbState:      persistancev1.PostMigrationInProgress,
							Type:         "aurora-postgres",
							DBVersion:    "13.11",
							Shape:        "db.t4g.medium",
							MinStorageGB: 20,
						},
					},
				},
			},
			M_NotSupported,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client:             tt.fields.Client,
				Log:                tt.fields.Log,
				Scheme:             tt.fields.Scheme,
				Config:             tt.fields.Config,
				MasterAuth:         tt.fields.MasterAuth,
				DbIdentifierPrefix: tt.fields.DbIdentifierPrefix,
				Mode:               tt.fields.Mode,
				Input:              tt.fields.Input,
				Class:              tt.fields.Class,
			}
			if got := r.getMode(tt.args.dbClaim); got != tt.want {
				t.Errorf("DatabaseClaimReconciler.getMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDatabaseClaimReconciler_BackupPolicy(t *testing.T) {
	type mockReconciler struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
		Config *viper.Viper
	}
	type args struct {
		dbClaim *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name       string
		reconciler mockReconciler
		args       args
		want       persistancev1.Tag
	}{
		{
			"Get default Backup Policy Setting",
			mockReconciler{
				Config: NewConfig(multiConfig),
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						BackupPolicy: "",
					},
				},
			},
			persistancev1.Tag{Key: defaultBackupPolicyKey, Value: "Bronze"},
		},
		{
			"Get Backup Policy set in DBClaim CR",
			mockReconciler{
				Config: NewConfig(multiConfig),
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						BackupPolicy: "Gold",
					},
				},
			},
			persistancev1.Tag{Key: defaultBackupPolicyKey, Value: "Gold"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client: tt.reconciler.Client,
				Log:    tt.reconciler.Log,
				Scheme: tt.reconciler.Scheme,
				Config: tt.reconciler.Config,
			}
			got := r.configureBackupPolicy(tt.args.dbClaim.Spec.BackupPolicy, tt.args.dbClaim.Spec.Tags)

			if got[0] != tt.want {
				t.Errorf("configureBackupPolicy() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_generateMasterPassword(t *testing.T) {
	tests := []struct {
		name    string
		want    int
		wantErr bool
	}{
		{
			"generateMasterPassword",
			30,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateMasterPassword()
			if (err != nil) != tt.wantErr {
				t.Errorf("generateMasterPassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) < tt.want {
				t.Errorf("generateMasterPassword() = %v len = %v, want len >= %v", got, len(got), tt.want)
			}
		})
	}
}

func TestManageMasterPassword(t *testing.T) {
	type mockReconciler struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
		Config *viper.Viper
	}
	type args struct {
		secret *xpv1.SecretKeySelector
	}
	tests := []struct {
		name       string
		reconciler mockReconciler
		args       args
		want       error
	}{
		{"use existing master secret",
			mockReconciler{
				Client: &mockClient{},
				Config: NewConfig(multiConfig),
				Log:    zap.New(zap.UseFlagOptions(&opts)),
			},
			args{
				secret: &xpv1.SecretKeySelector{
					SecretReference: xpv1.SecretReference{
						Name:      "sample-master-secret",
						Namespace: "testNamespace",
					},
					Key: "password",
				},
			},
			nil,
		},
		{"create master secret",
			mockReconciler{
				Client: &mockClient{},
				Config: NewConfig(multiConfig),
				Log:    zap.New(zap.UseFlagOptions(&opts)),
			},
			args{
				secret: &xpv1.SecretKeySelector{
					SecretReference: xpv1.SecretReference{
						Name:      "create-master-secret",
						Namespace: "testNamespace",
					},
					Key: "password",
				},
			},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client: tt.reconciler.Client,
				Log:    tt.reconciler.Log,
				Scheme: tt.reconciler.Scheme,
				Config: tt.reconciler.Config,
			}
			got := r.manageMasterPassword(context.Background(), tt.args.secret)
			assert.NoError(t, got)
		})
	}
}
