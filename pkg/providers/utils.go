package providers

import (
	"context"
	"fmt"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	gopassword "github.com/sethvargo/go-password/password"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

// ManageMasterPassword ensures the master password secret exists.
// It creates a new one if it doesn't exist.
func ManageMasterPassword(ctx context.Context, secret *xpv1.SecretKeySelector, k8Client client.Client) error {
	password, err := generateMasterPassword()
	if err != nil {
		return fmt.Errorf("failed to generate master password: %w", err)
	}

	masterSecret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Name:      secret.SecretReference.Name,
		Namespace: secret.SecretReference.Namespace,
	}

	if err := k8Client.Get(ctx, secretKey, masterSecret); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get master secret: %w", err)
		}

		masterSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: secret.SecretReference.Namespace,
				Name:      secret.SecretReference.Name,
			},
			Data: map[string][]byte{
				secret.Key: []byte(password),
			},
		}
		return k8Client.Create(ctx, masterSecret)
	}

	return nil
}

func generateMasterPassword() (string, error) {
	var pass string
	var err error
	minPasswordLength := 30

	pass, err = gopassword.Generate(minPasswordLength, 3, 0, false, true)
	if err != nil {
		return "", err
	}
	return pass, nil
}

func GetEngineVersion(params hostparams.HostParams, config *viper.Viper) *string {
	defaultMajorVersion := ""
	if params.IsDefaultVersion {
		defaultMajorVersion = basefun.GetDefaultMajorVersion(config)
	} else {
		defaultMajorVersion = params.DBVersion
	}
	return &defaultMajorVersion
}

func getParameterGroupName(providerResourceName, dbVersion string, dbType v1.DatabaseType) string {
	switch dbType {
	case v1.Postgres:
		return providerResourceName + "-" + (strings.Split(dbVersion, "."))[0]
	case v1.AuroraPostgres:
		return providerResourceName + "-a-" + (strings.Split(dbVersion, "."))[0]
	default:
		return providerResourceName + "-" + (strings.Split(dbVersion, "."))[0]
	}
}

func isReady(cond []xpv1.Condition) (bool, error) {
	// Check if the cluster is marked as ready in its status conditions
	for _, condition := range cond {
		if condition.Type == xpv1.TypeReady && condition.Status == corev1.ConditionTrue {
			return true, nil
		}
		if condition.Reason == xpv1.ReasonReconcileError {
			return false, fmt.Errorf("crossplane resource is in a bad state: %s", condition.Message)
		}
	}
	return false, nil
}
