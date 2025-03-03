package providers

import (
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"testing"
)

func TestGetParameterGroupName(t *testing.T) {
	tests := []struct {
		name                  string
		providerResourceName  string
		dbVersion             string
		dbType                v1.DatabaseType
		expectedParameterName string
	}{
		{
			name:                  "Postgres DB",
			providerResourceName:  "env-app-name-db-1d9fb87",
			dbVersion:             "14.5",
			dbType:                v1.Postgres,
			expectedParameterName: "env-app-name-db-1d9fb87-14",
		},
		{
			name:                  "Aurora Postgres DB",
			providerResourceName:  "env-app-name-db-1d9fb87",
			dbVersion:             "13.3",
			dbType:                v1.AuroraPostgres,
			expectedParameterName: "env-app-name-db-1d9fb87-a-13",
		},
		{
			name:                  "Malformed version, should still extract major version",
			providerResourceName:  "env-app-name-db-1d9fb87",
			dbVersion:             "9",
			dbType:                v1.Postgres,
			expectedParameterName: "env-app-name-db-1d9fb87-9",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := getParameterGroupName(test.providerResourceName, test.dbVersion, test.dbType)
			if result != test.expectedParameterName {
				t.Errorf("[%s] expected: %s, got: %s", test.name, test.expectedParameterName, result)
			}
		})
	}
}
