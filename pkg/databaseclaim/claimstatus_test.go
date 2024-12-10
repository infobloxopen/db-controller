package databaseclaim

import (
	"context"
	"testing"
	"time"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/infobloxopen/db-controller/pkg/hostparams"
	role "github.com/infobloxopen/db-controller/pkg/roleclaim"
)

func TestSuccessAndUpdateCondition(t *testing.T) {
	tests := []struct {
		name              string
		deletionTimestamp *metav1.Time
		oldDBState        v1.DbState
		expectedRequeue   bool
		expectedRequeueAfter time.Duration
		expectError       bool
	}{
		{
			name:              "Success case, requeue after the configured password rotation time",
			deletionTimestamp: nil,
			expectedRequeue:   false,
			expectedRequeueAfter: time.Minute * 5,
			expectError:       false,
		},
		{
			name:              "Object is being deleted, then call requeue immediately",
			deletionTimestamp: &metav1.Time{Time: time.Now()},
			expectedRequeue:   true,
			expectError:       false,
		},
		{
			name:              "PostMigrationInProgress, then requeue after one minute",
			deletionTimestamp: nil,
			oldDBState:        v1.PostMigrationInProgress,
			expectedRequeue:   false,
			expectedRequeueAfter: time.Minute,
			expectError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := role.MockClient{}
			m := &manager{
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

			condType := "Ready"
			result, err := m.SuccessAndUpdateCondition(context.Background(), dbClaim, condType)

			if tt.expectError && err == nil {
				t.Errorf("expected an error but got nil")
			}
			
			if !tt.expectError && err != nil {
				t.Errorf("did not expect an error but got: %v", err)
			}
			
			if tt.expectedRequeue && !result.Requeue {
				t.Errorf("expected Requeue to be true but got false")
			}
			
			if !tt.expectedRequeue && result.Requeue {
				t.Errorf("expected Requeue to be false but got true")
			}
			
			if tt.expectedRequeueAfter != 0 && result.RequeueAfter != tt.expectedRequeueAfter {
				t.Errorf("expected RequeueAfter to be %v but got %v", tt.expectedRequeueAfter, result.RequeueAfter)
			}
		})
	}
}

func TestUpdateClusterStatus(t *testing.T) {
	m := &manager{}
	status := &v1.Status{}
	hostParams := &hostparams.HostParams{
		DBVersion:    "12.7",
		Type:         "postgres",
		Shape:        "micro",
		MinStorageGB: 10,
		MaxStorageGB: 50,
	}

	m.UpdateClusterStatus(status, hostParams)

	if status.DBVersion != "12.7" {
		t.Errorf("expected DBVersion to be '12.7', got '%s'", status.DBVersion)
	}
	if status.Type != v1.Postgres {
		t.Errorf("expected Type to be 'Postgres', got '%s'", status.Type)
	}
	if status.Shape != "micro" {
		t.Errorf("expected Shape to be 'micro', got '%s'", status.Shape)
	}
	if status.MinStorageGB != 10 {
		t.Errorf("expected MinStorageGB to be 10, got '%d'", status.MinStorageGB)
	}
	if status.MaxStorageGB != 50 {
		t.Errorf("expected MaxStorageGB to be 50, got '%d'", status.MaxStorageGB)
	}
}

func TestUpdateDBStatus(t *testing.T) {
	m := &manager{}
	status := &v1.Status{}
	dbName := "testdb"

	m.UpdateDBStatus(status, dbName)

	if status.ConnectionInfo.DatabaseName != "testdb" {
		t.Errorf("expected DatabaseName to be 'testdb', got '%s'", status.ConnectionInfo.DatabaseName)
	}

	if status.ConnectionInfoUpdatedAt == nil {
		t.Errorf("expected ConnectionInfoUpdatedAt to be set, got nil")
	}
}

func TestUpdateHostPortStatus(t *testing.T) {
	m := &manager{}
	status := &v1.Status{}
	host := "127.0.0.1"
	port := "5432"
	sslMode := "require"

	m.UpdateHostPortStatus(status, host, port, sslMode)

	if status.ConnectionInfo.Host != host {
		t.Errorf("expected Host to be '%s', got '%s'", host, status.ConnectionInfo.Host)
	}

	if status.ConnectionInfo.Port != port {
		t.Errorf("expected Port to be '%s', got '%s'", port, status.ConnectionInfo.Port)
	}

	if status.ConnectionInfo.SSLMode != sslMode {
		t.Errorf("expected SSLMode to be '%s', got '%s'", sslMode, status.ConnectionInfo.SSLMode)
	}

	if status.ConnectionInfoUpdatedAt == nil {
		t.Errorf("expected ConnectionInfoUpdatedAt to be set, got nil")
	}
}

func TestUpdateUserStatus(t *testing.T) {
	m := &manager{}
	status := &v1.Status{}
	reqInfo := &requestInfo{}
	userName := "user"
	userPassword := "pwd"

	m.UpdateUserStatus(status, reqInfo, userName, userPassword)

	if status.ConnectionInfo.Username != userName {
		t.Errorf("expected Username to be '%s', got '%s'", userName, status.ConnectionInfo.Username)
	}

	if status.UserUpdatedAt == nil {
		t.Errorf("expected UserUpdatedAt to be set, got nil")
	}

	if reqInfo.TempSecret != userPassword {
		t.Errorf("expected TempSecret to be '%s', got '%s'", userPassword, reqInfo.TempSecret)
	}

	if status.ConnectionInfoUpdatedAt == nil {
		t.Errorf("expected ConnectionInfoUpdatedAt to be set, got nil")
	}
}
