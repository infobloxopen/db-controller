package databaseclaim

import (
	"errors"
	"testing"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	"k8s.io/utils/ptr"
)

func TestExistingDSN(t *testing.T) {

	for _, tt := range []struct {
		name       string
		user       string
		dsn        string
		expected   string
		secretData map[string][]byte
		error      error
	}{
		{
			name:       "empty",
			dsn:        "",
			expected:   "",
			secretData: map[string][]byte{},
			error:      ErrInvalidCredentialsPasswordMissing,
		},
		{
			user: "postgres",
			name: "no dsn w/no secret password",
			secretData: map[string][]byte{
				"dsn.txt": []byte("postgres://postgres:dsnPassword@localhost:5432/postgres?sslmode=disable"),
			},
			dsn:      "postgres://postgres@localhost:5432/postgres?sslmode=disable",
			expected: "postgres://postgres:dsnPassword@localhost:5432/postgres?sslmode=disable",
		},
		{
			name: "dsn with secret password",
			user: "postgres",
			dsn:  "postgres://localhost:5432/postgres?sslmode=disable",
			secretData: map[string][]byte{
				"password": []byte("secretPassword"),
			},
			expected: "postgres://postgres:secretPassword@localhost:5432/postgres?sslmode=disable",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			claim := &v1.DatabaseClaim{
				Spec: v1.DatabaseClaimSpec{
					Username:          tt.user,
					UseExistingSource: ptr.To(true),
					SourceDataFrom: &v1.SourceDataFrom{
						Type: "database",
						Database: &v1.Database{
							DSN: tt.dsn,
						},
					},
				},
			}
			actual, err := parseExistingDSN(tt.secretData, claim)
			if !errors.Is(err, tt.error) {
				t.Errorf("got: %v, wanted: %v", err, tt.error)
			}
			if actual.Uri() != tt.expected {
				t.Errorf("\n   got: %q\nwanted: %q", actual.Uri(), tt.expected)
			}
		})
	}
}
