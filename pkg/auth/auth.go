package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/kctlutils"
)

// Creds contains the many authentications available during a
// request
type Creds struct {
	SourceDSN, TargetDSN   string
	SourceUser, TargetUser string
}

// FromContext returns the Creds from the context
func FromContext(ctx context.Context) *Creds {
	return ctx.Value("creds").(*Creds)
}

// NewContext returns a new context with the Creds
func NewContext(ctx context.Context, creds *Creds) context.Context {
	return context.WithValue(ctx, "creds", creds)
}

// UpdateDSNWithPassword updates the DSN with the password
func UpdateDSNWithPassword(dsn, password string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword(u.User.Username(), password)
	return u.String(), nil
}

// PopulateCreds attempts to find creds from the variety of places
// they can be found in
func PopulateCreds(ctx context.Context, cli client.Reader, dbClaim *v1.DatabaseClaim, serviceNS string) (*Creds, error) {

	secret := &corev1.Secret{}
	err := cli.Get(ctx, client.ObjectKey{
		Namespace: dbClaim.Namespace,
		Name:      dbClaim.Spec.SecretName,
	}, secret)
	if err != nil {
		return nil, err
	}

	srcAdmin, err := fetchAdminFromSourceDataFrom(ctx, cli, dbClaim)
	if err != nil {
		srcAdmin, err = fetchAdminFromActiveDB(ctx, cli, dbClaim, serviceNS)
		if err != nil {
			return nil, err
		}
	}

	return &Creds{
		SourceDSN: srcAdmin,
	}, nil
}

// GetSourceDataFromDSN returns the DSN from the source data from
// DSN field or secretRef.
func GetSourceDataFromDSN(ctx context.Context, cli client.Reader, dbClaim *v1.DatabaseClaim) (string, error) {
	return fetchAdminFromSourceDataFrom(ctx, cli, dbClaim)
}

// fetchAdminFromSourceDataFrom reads the raw DSN and secretRef
func fetchAdminFromSourceDataFrom(ctx context.Context, cli client.Reader, dbClaim *v1.DatabaseClaim) (string, error) {
	var srcAdmin string
	// Fetch source from sourcedatafrom location
	if dbClaim.Spec.SourceDataFrom != nil {
		srcAdmin = dbClaim.Spec.SourceDataFrom.Database.DSN
		if srcAdmin == "" {
			info, err := getSourceDataFromDSN(ctx, cli, dbClaim)
			if err != nil {
				return "", err
			}
			return info.Uri(), nil
		}
	}
	return srcAdmin, nil
}

func fetchAdminFromActiveDB(ctx context.Context, cli client.Reader, dbClaim *v1.DatabaseClaim, serviceNS string) (string, error) {
	if dbClaim.Status.ActiveDB.ConnectionInfo != nil && dbClaim.Status.ActiveDB.ConnectionInfo.Uri() != "" {

		activeHost, _, _ := strings.Cut(dbClaim.Status.ActiveDB.ConnectionInfo.Host, ".")

		kctl := kctlutils.New(cli, serviceNS)
		connInfo, err := kctl.GetMasterCredsDeprecated(ctx, activeHost, dbClaim.Spec.DatabaseName, dbClaim.Status.ActiveDB.ConnectionInfo.SSLMode)
		if err != nil {
			return "", err
		}
		return connInfo.Uri(), nil
	}
	return "", errors.New("unable to fetch source admin")
}
