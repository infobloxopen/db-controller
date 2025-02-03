package databaseclaim

import (
	"context"
	"github.com/stretchr/testify/assert"
	"testing"

	"github.com/aws/smithy-go/ptr"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
)

func createMockRequestInfo(sharedDBHost bool) *requestInfo {
	return &requestInfo{
		SharedDBHost: sharedDBHost,
		HostParams:   hostparams.HostParams{},
	}
}

func createMockDatabaseClaim(status v1.DatabaseClaimStatus, spec v1.DatabaseClaimSpec) *v1.DatabaseClaim {
	return &v1.DatabaseClaim{
		Status: status,
		Spec:   spec,
	}
}

func TestGetMode(t *testing.T) {
	tests := []struct {
		name     string
		reqInfo  *requestInfo
		dbClaim  *v1.DatabaseClaim
		expected ModeEnum
	}{
		{
			name:    "Given old DB is in post migration state and active DB is ready, then state is Post migration in progress",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(v1.DatabaseClaimStatus{
				OldDB:    v1.StatusForOldDB{DbState: v1.PostMigrationInProgress, ConnectionInfo: nil},
				ActiveDB: v1.Status{DbState: v1.Ready},
			},
				v1.DatabaseClaimSpec{},
			),
			expected: M_NotSupported,
		},
		{
			name:    "Given the request has SharedDBHost=true, then state is use new DB",
			reqInfo: createMockRequestInfo(true),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{},
				v1.DatabaseClaimSpec{},
			),
			expected: M_UseNewDB,
		},
		{
			name:    "Given the Old DB is migrated the Active DB is ready, then the state is post migration",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{
					OldDB:    v1.StatusForOldDB{DbState: v1.PostMigrationInProgress, ConnectionInfo: &v1.DatabaseClaimConnectionInfo{}},
					ActiveDB: v1.Status{DbState: v1.Ready},
				},
				v1.DatabaseClaimSpec{},
			),
			expected: M_PostMigrationInProgress,
		},
		{
			name:    "Given db claim has UseExistingSource set with source data type database, then the state is use existing DB",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{},
				v1.DatabaseClaimSpec{UseExistingSource: ptr.Bool(true), SourceDataFrom: &v1.SourceDataFrom{Type: "database"}},
			),
			expected: M_UseExistingDB,
		},
		{
			name:    "Given source data from is set with type which is not database, the sate should be unsuported",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{},
				v1.DatabaseClaimSpec{SourceDataFrom: &v1.SourceDataFrom{Type: "base"}},
			),
			expected: M_NotSupported,
		},
		{
			name:    "Migration existing to new DB",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{ActiveDB: v1.Status{DbState: v1.UsingExistingDB}, MigrationState: "create_subscription"},
				v1.DatabaseClaimSpec{SourceDataFrom: &v1.SourceDataFrom{Type: "database"}},
			),
			expected: M_MigrationInProgress,
		},
		{
			name:    "Migration in Progress",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{ActiveDB: v1.Status{DbState: v1.UsingExistingDB}, MigrationState: ""},
				v1.DatabaseClaimSpec{SourceDataFrom: &v1.SourceDataFrom{Type: "database"}},
			),
			expected: M_MigrateExistingToNewDB,
		},
		{
			name:    "Source Data Present, Initial Migration State",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{
					ActiveDB:       v1.Status{DbState: v1.UsingExistingDB, SourceDataFrom: &v1.SourceDataFrom{Type: "database"}},
					MigrationState: "initial",
				},
				v1.DatabaseClaimSpec{},
			),
			expected: M_MigrateExistingToNewDB,
		},
		{
			name:    "Source Data Present, Migration In Progress",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{
					ActiveDB:       v1.Status{DbState: v1.UsingExistingDB, SourceDataFrom: &v1.SourceDataFrom{Type: "database"}},
					MigrationState: "in-progress",
				},
				v1.DatabaseClaimSpec{},
			),
			expected: M_MigrationInProgress,
		},
		{
			name:    "Source Data Absent, Active DB State Using Existing, SourceDataFrom is Nil",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{
					ActiveDB: v1.Status{DbState: v1.UsingExistingDB},
				},
				v1.DatabaseClaimSpec{},
			),
			expected: M_NotSupported,
		},
		{
			name:    "Source Data Absent, Active DB State Not Using Existing",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{
					ActiveDB: v1.Status{DbState: v1.Ready},
				},
				v1.DatabaseClaimSpec{},
			),
			expected: M_UseNewDB,
		},
		{
			name:    "Source Data Present, Migration State Completed",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{
					ActiveDB:       v1.Status{DbState: v1.UsingExistingDB, SourceDataFrom: &v1.SourceDataFrom{Type: "database"}},
					MigrationState: "completed",
				},
				v1.DatabaseClaimSpec{},
			),
			expected: M_UseNewDB,
		},
		{
			name:    "Missing Required Fields for SourceDataFrom",
			reqInfo: createMockRequestInfo(false),
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{
					ActiveDB:       v1.Status{DbState: v1.UsingExistingDB, SourceDataFrom: nil},
					MigrationState: "initial",
				},
				v1.DatabaseClaimSpec{},
			),
			expected: M_NotSupported,
		},
		{
			name: "Active DB State Ready, Upgrade Requested, NewDB State Empty, MigrationState Empty, version changed - upgrade request",
			reqInfo: &requestInfo{
				SharedDBHost: false,
				HostParams:   hostparams.HostParams{DBVersion: "15.1"},
			},
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{
					ActiveDB:       v1.Status{DbState: v1.Ready, DBVersion: "16.1"},
					NewDB:          v1.Status{DbState: ""},
					MigrationState: "",
				},
				v1.DatabaseClaimSpec{},
			),
			expected: M_InitiateDBUpgrade, // there are actually 3 cases for upgrade request - version bump, engine change, instance class changed
		},
		{
			name: "Source Data Absent, Active DB State Ready, Upgrade Requested, MigrationState Initial",
			reqInfo: &requestInfo{
				SharedDBHost: false,
				HostParams:   hostparams.HostParams{DBVersion: "15.1"},
			},
			dbClaim: createMockDatabaseClaim(
				v1.DatabaseClaimStatus{
					ActiveDB:       v1.Status{DbState: v1.Ready, DBVersion: "16.1"},
					NewDB:          v1.Status{DbState: "anything"},
					MigrationState: "copy_schema",
				},
				v1.DatabaseClaimSpec{},
			),
			expected: M_UpgradeDBInProgress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMode(context.Background(), tt.reqInfo, tt.dbClaim)
			assert.Equal(t, tt.expected, got)
		})
	}
}
