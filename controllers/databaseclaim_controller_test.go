package controllers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
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

func TestDatabaseClaimReconcilerGeneratePassword(t *testing.T) {
	type reconciler struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
		Config *viper.Viper
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
	client.Reader
	client.Writer
	client.StatusClient
}

func (m mockClient) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	_ = ctx
	if key.Namespace == "testNamespace" && key.Name == "sample-master-secret" {
		sec, ok := obj.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("can't assert type")
		}
		sec.Data = map[string][]byte{
			"password": []byte("masterpassword"),
		}
		return nil
	}

	return fmt.Errorf("not found")
}

func TestDatabaseClaimReconcilerReadMasterPassword(t *testing.T) {
	type mockReconciler struct {
		Client client.Client
		Log    logr.Logger
		Scheme *runtime.Scheme
		Config *viper.Viper
	}
	type args struct {
		ctx         context.Context
		fragmentKey string
		namespace   string
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
		t.Run(tt.name, func(t *testing.T) {
			r := &DatabaseClaimReconciler{
				Client: tt.reconciler.Client,
				Log:    tt.reconciler.Log,
				Scheme: tt.reconciler.Scheme,
				Config: tt.reconciler.Config,
			}
			got, err := r.readMasterPassword(tt.args.ctx, tt.args.fragmentKey, tt.args.namespace)
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

var secretRef = []byte(`
    sample-connection:
      PasswordSecretRef: sample-master-secret
`)

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
    sample-connection:
      Username: postgres
      Host: db-controller-postgresql
      Port: 5432
      useSSL: false
      PasswordSecretRef: sample-master-secret
`)

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
				},
			},
			[]string{
				"db-controller-postgresql",
				"overridden-host",
				"5432",
				"1234",
				"postgres",
			},
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
			t.Log("getMasterHost() Host from testConfig")
			if got := r.getMasterHost(tt.args[0].fragmentKey, tt.args[0].dbClaim); got != tt.want[0] {
				t.Errorf("getMasterHost() = %v, want %v", got, tt.want[0])
			}
			t.Log("getMasterHost() Host from testConfig PASS")

			t.Log("getMasterHost() Host overridden by DB claim")
			if got := r.getMasterHost(tt.args[1].fragmentKey, tt.args[1].dbClaim); got != tt.want[1] {
				t.Errorf("getMasterPort() = %v, want %v", got, tt.want[1])
			}
			t.Log("getMasterPort() Host overridden by DB claim PASS")

			t.Log("getMasterHost() Port from testConfig")
			if got := r.getMasterPort(tt.args[2].fragmentKey, tt.args[2].dbClaim); got != tt.want[2] {
				t.Errorf("getMasterPort() = %v, want %v", got, tt.want[2])
			}
			t.Log("getMasterPort() Port from testConfig PASS")

			t.Log("getMasterHost() Port overridden by DB claim")
			if got := r.getMasterPort(tt.args[3].fragmentKey, tt.args[3].dbClaim); got != tt.want[3] {
				t.Errorf("getMasterPort() = %v, want %v", got, tt.want[3])
			}
			t.Log("getMasterPort() Port overridden by DB claim PASS")

			if got := r.getMasterUser(tt.args[4].fragmentKey); got != tt.want[4] {
				t.Errorf("getMasterUser() = %v, want %v", got, tt.want[4])
			}
			t.Log("getMasterUser() PASS")
		})
	}
}

var sslModeDisabled = []byte(`
    sample-connection:
      useSSL: false
`)

var sslModeEnabled = []byte(`
    sample-connection:
      useSSL: true
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
			args{fragmentKey: "sample-connection"},
			"disable",
		},
		{
			"Get master connection params Host, Port, user name",
			mockReconciler{
				Config: NewConfig(sslModeEnabled),
			},
			args{fragmentKey: "sample-connection"},
			"enable",
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
			if got := r.getSSLMode(tt.args.fragmentKey); got != tt.want {
				t.Errorf("getSSLMode() = %v, want %v", got, tt.want)
			}
			t.Log("getSSLMode() PASS")
		})
	}
}

var multiConfig = []byte(`
    sample:
      Host: sample.Host
    sample.connection:
      Host: test.Host
    another.connection:
      Host: another.Host
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
						InstanceLabel: "sample.connection",
					},
					Status: persistancev1.DatabaseClaimStatus{
						ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{},
					},
				},
			},
			"sample.connection",
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
						ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{},
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

var passwordRotationGt1440 = []byte(`
    passwordConfig:
      passwordRotationPeriod: "2000"
`)

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

func TestDatabaseClaimReconcilerIsUserChanged(t *testing.T) {
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
		want       bool
	}{
		{
			"User unchanged",
			mockReconciler{
				Config: NewConfig(multiConfig),
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						Username: "oldUser",
					},
					Status: persistancev1.DatabaseClaimStatus{
						ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{
							Username: "oldUser",
						},
					},
				},
			},
			false,
		},
		{
			"User unchanged",
			mockReconciler{
				Config: NewConfig(multiConfig),
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						Username: "oldUser",
					},
					Status: persistancev1.DatabaseClaimStatus{
						ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{
							Username: "",
						},
					},
				},
			},
			false,
		},
		{
			"User changed",
			mockReconciler{
				Config: NewConfig(multiConfig),
			},
			args{
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						Username: "newUser",
					},
					Status: persistancev1.DatabaseClaimStatus{
						ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{
							Username: "oldUser",
						},
					},
				},
			},
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
			if got := r.isUserChanged(tt.args.dbClaim); got != tt.want {
				t.Errorf("isUserChanged() = %v, want %v", got, tt.want)
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
