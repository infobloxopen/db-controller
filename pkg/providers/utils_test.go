package providers

import (
	"testing"
)

func TestGetParameterGroupName(t *testing.T) {
	tests := []struct {
		name                  string
		providerResourceName  string
		dbVersion             string
		dbType                string
		expectedParameterName string
	}{
		{
			name:                  "Postgres DB",
			providerResourceName:  "env-app-name-db-1d9fb87",
			dbVersion:             "14.5",
			dbType:                AwsPostgres,
			expectedParameterName: "env-app-name-db-1d9fb87-14",
		},
		{
			name:                  "Aurora Postgres DB",
			providerResourceName:  "env-app-name-db-1d9fb87",
			dbVersion:             "13.3",
			dbType:                AwsAuroraPostgres,
			expectedParameterName: "env-app-name-db-1d9fb87-a-13",
		},
		{
			name:                  "Malformed version, should still extract major version",
			providerResourceName:  "env-app-name-db-1d9fb87",
			dbVersion:             "9",
			dbType:                AwsPostgres,
			expectedParameterName: "env-app-name-db-1d9fb87-9",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := getParameterGroupName(DatabaseSpec{ResourceName: test.providerResourceName, DBVersion: test.dbVersion, DbType: test.dbType})
			if result != test.expectedParameterName {
				t.Errorf("[%s] expected: %s, got: %s", test.name, test.expectedParameterName, result)
			}
		})
	}
}
