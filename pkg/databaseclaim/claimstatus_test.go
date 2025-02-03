package databaseclaim

import (
	"context"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/infobloxopen/db-controller/pkg/hostparams"
	role "github.com/infobloxopen/db-controller/pkg/roleclaim"
)

func TestSuccessAndUpdateCondition(t *testing.T) {
	tests := []struct {
		name                 string
		deletionTimestamp    *metav1.Time
		oldDBState           v1.DbState
		expectedRequeue      bool
		expectedRequeueAfter time.Duration
		expectError          bool
	}{
		{
			name:                 "Success case, requeue after the configured password rotation time",
			deletionTimestamp:    nil,
			expectedRequeue:      false,
			expectedRequeueAfter: time.Minute * 5,
			expectError:          false,
		},
		{
			name:              "Object is being deleted, then call requeue immediately",
			deletionTimestamp: &metav1.Time{Time: time.Now()},
			expectedRequeue:   true,
			expectError:       false,
		},
		{
			name:                 "PostMigrationInProgress, then requeue after one minute",
			deletionTimestamp:    nil,
			oldDBState:           v1.PostMigrationInProgress,
			expectedRequeue:      false,
			expectedRequeueAfter: time.Minute,
			expectError:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := role.MockClient{}
			m := &StatusManager{
				client:               &mock,
				passwordRotationTime: time.Minute * 5,
			}

			dbClaim := &v1.DatabaseClaim{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: tt.deletionTimestamp,
				},
				Status: v1.DatabaseClaimStatus{
					OldDB: v1.StatusForOldDB{
						DbState: tt.oldDBState,
					},
				},
			}

			result, err := m.SuccessAndUpdateCondition(context.Background(), dbClaim)

			if tt.expectError {
				assert.Error(t, err)
			}

			if !tt.expectError {
				assert.NoError(t, err)
			}

			if tt.expectedRequeue {
				assert.True(t, result.Requeue)
			}

			if !tt.expectedRequeue {
				assert.False(t, result.Requeue)
			}

			if tt.expectedRequeueAfter != 0 {
				assert.Equal(t, tt.expectedRequeueAfter, result.RequeueAfter)
			}
		})
	}
}

func TestUpdateClusterStatus(t *testing.T) {
	m := &StatusManager{}
	status := &v1.Status{}
	hostParams := &hostparams.HostParams{
		DBVersion:    "12.7",
		Type:         "postgres",
		Shape:        "micro",
		MinStorageGB: 10,
		MaxStorageGB: 50,
	}

	m.UpdateClusterStatus(status, hostParams)
	assert.Equal(t, "12.7", status.DBVersion)
	assert.Equal(t, v1.Postgres, status.Type)
	assert.Equal(t, "micro", status.Shape)
	assert.Equal(t, 10, status.MinStorageGB)
	assert.Equal(t, int64(50), status.MaxStorageGB)
}

func TestUpdateDBStatus(t *testing.T) {
	m := &StatusManager{}
	status := &v1.Status{}
	dbName := "testdb"

	m.UpdateDBStatus(status, dbName)

	assert.Equal(t, dbName, status.ConnectionInfo.DatabaseName, "DatabaseName should match")
	assert.NotNil(t, status.ConnectionInfoUpdatedAt, "ConnectionInfoUpdatedAt should be set")
}

func TestUpdateHostPortStatus(t *testing.T) {
	m := &StatusManager{}
	status := &v1.Status{}
	host := "127.0.0.1"
	port := "5432"
	sslMode := "require"

	m.UpdateHostPortStatus(status, host, port, sslMode)

	assert.Equal(t, host, status.ConnectionInfo.Host, "Host should match")
	assert.Equal(t, port, status.ConnectionInfo.Port, "Port should match")
	assert.Equal(t, sslMode, status.ConnectionInfo.SSLMode, "SSLMode should match")
	assert.NotNil(t, status.ConnectionInfoUpdatedAt, "ConnectionInfoUpdatedAt should be set")
}

func TestUpdateUserStatus(t *testing.T) {
	m := &StatusManager{}
	status := &v1.Status{}
	reqInfo := &requestInfo{}
	userName := "user"
	userPassword := "pwd"

	m.UpdateUserStatus(status, reqInfo, userName, userPassword)

	assert.Equal(t, userName, status.ConnectionInfo.Username, "Username should match")
	assert.NotNil(t, status.UserUpdatedAt, "UserUpdatedAt should be set")
	assert.Equal(t, userPassword, reqInfo.TempSecret, "TempSecret should match")
	assert.NotNil(t, status.ConnectionInfoUpdatedAt, "ConnectionInfoUpdatedAt should be set")
}
