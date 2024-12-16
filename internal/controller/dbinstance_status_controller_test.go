package controller_test

//import (
//	"context"
//	"testing"
//
//	"github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
//	"github.com/go-logr/logr"
//	persistencev1 "github.com/infobloxopen/db-controller/api/v1"
//	"github.com/stretchr/testify/assert"
//	"github.com/stretchr/testify/mock"
//	"github.com/stretchr/testify/suite"
//	v1 "k8s.io/api/core/v1"
//	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
//	"k8s.io/apimachinery/pkg/runtime"
//	"sigs.k8s.io/controller-runtime/pkg/client"
//	"sigs.k8s.io/controller-runtime/pkg/reconcile"
//)
//
//const (
//	ConditionReady            = "Ready"
//	ConditionSynced           = "Synced"
//	ConditionReadyAtProvider  = "ReadyAtProvider"
//	ConditionSyncedAtProvider = "SyncedAtProvider"
//)
//
//// MockClient for simulating the client
//type MockClient struct {
//	mock.Mock
//	client.Client
//}
//
//func (m *MockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
//	args := m.Called(ctx, key, obj, opts)
//	return args.Error(0)
//}
//
//func (m *MockClient) Status() client.StatusWriter {
//	args := m.Called()
//	return args.Get(0).(client.StatusWriter)
//}
//
//// Simulated reconciler
//type DBInstanceStatusReconciler struct {
//	client.Client
//	Scheme *runtime.Scheme
//}
//
//// updateDatabaseClaimStatus updates the status of the DatabaseClaim based on the DBInstance conditions
//func (m *DBInstanceStatusReconciler) updateDatabaseClaimStatus(ctx context.Context, dbInstance *v1alpha1.DBInstance, dbClaim *persistencev1.DatabaseClaim, logger logr.Logger) error {
//	updated := false
//
//	// Iterate over DBInstance conditions
//	for _, condition := range dbInstance.Status.Conditions {
//
//		if condition.Type == ConditionReady && condition.Status == v1.ConditionTrue {
//
//			newCondition := metav1.Condition{
//				Type:               ConditionReadyAtProvider,
//				Status:             metav1.ConditionStatus(v1.ConditionTrue), // Convert to the correct type
//				LastTransitionTime: condition.LastTransitionTime,
//				Reason:             string(condition.Reason),
//				Message:            condition.Message,
//			}
//			conditionExists := false
//			for _, existingCondition := range dbClaim.Status.Conditions {
//				if existingCondition.Type == newCondition.Type {
//					conditionExists = true
//					break
//				}
//			}
//			if !conditionExists {
//				dbClaim.Status.Conditions = append(dbClaim.Status.Conditions, newCondition)
//				updated = true
//			}
//
//			break
//		}
//	}
//	if updated {
//		if err := m.Status().Update(ctx, dbClaim); err != nil {
//			return err
//		}
//	}
//
//	return nil
//}
//
//type DBInstanceStatusReconcilerTestSuite struct {
//	suite.Suite
//	mockClient *MockClient
//}
//
//func (suite *DBInstanceStatusReconcilerTestSuite) SetupTest() {
//	// Initializing MockClient before each test
//	suite.mockClient = new(MockClient)
//}
//
//func (suite *DBInstanceStatusReconcilerTestSuite) TearDownTest() {
//	// Verify if all mocks were used correctly after the test
//	suite.mockClient.AssertExpectations(suite.T())
//}
//
//func (suite *DBInstanceStatusReconcilerTestSuite) TestReconcile() {
//	// Setup
//	mockClient := new(MockClient)
//	reconciler := &DBInstanceStatusReconciler{
//		Client: mockClient,
//		Scheme: nil,
//	}
//
//	req := reconcile.Request{
//		NamespacedName: client.ObjectKey{
//			Name:      "test-db-instance",
//			Namespace: "default",
//		},
//	}
//
//	dbInstance := &v1alpha1.DBInstance{
//		ObjectMeta: metav1.ObjectMeta{
//			Name:      "test-db-instance",
//			Namespace: "default",
//			Labels: map[string]string{
//				"app.kubernetes.io/instance": "test-db-claim",
//			},
//		},
//		Status: v1alpha1.DBInstanceStatus{
//			Conditions: []metav1.Condition{
//				{
//					Type:   ConditionReady,
//					Status: v1.ConditionTrue,
//				},
//			},
//		},
//	}
//
//	dbClaim := &persistencev1.DatabaseClaim{
//		ObjectMeta: metav1.ObjectMeta{
//			Name:      "test-db-claim",
//			Namespace: "default",
//		},
//		Status: persistencev1.DatabaseClaimStatus{
//			Conditions: []metav1.Condition{}, // Starting with no conditions
//		},
//	}
//
//	// Define the mock client behavior
//	mockClient.On("Get", mock.Anything, req.NamespacedName, dbInstance, mock.Anything).Return(nil)
//	mockClient.On("Get", mock.Anything, client.ObjectKey{Name: "test-db-claim", Namespace: "default"}, dbClaim, mock.Anything).Return(nil)
//	mockClient.On("Status").Return(mockClient)
//
//	// Run the Reconcile function
//	_, err := reconciler.Reconcile(context.Background(), req)
//
//	// Check that there was no error
//	assert.NoError(suite.T(), err)
//	mockClient.AssertExpectations(suite.T())
//
//	// Verify that the DatabaseClaim status was updated
//	assert.Len(suite.T(), dbClaim.Status.Conditions, 1)
//	assert.Equal(suite.T(), ConditionReadyAtProvider, dbClaim.Status.Conditions[0].Type)
//}
//
//// Setup test suite
//func TestDBInstanceStatusReconciler(t *testing.T) {
//	suite.Run(t, new(DBInstanceStatusReconcilerTestSuite))
//}
