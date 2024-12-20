package controller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/aws/smithy-go/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("DBInstanceStatusReconciler", func() {
	var (
		dbInstance *v1alpha1.DBInstance
		dbClaim    *persistancev1.DatabaseClaim
	)

	// Prepare resources for each test case
	BeforeEach(func() {

		// Create a DBInstance resource with required labels
		dbInstance = &v1alpha1.DBInstance{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "default-dbinstance",
				Labels: map[string]string{
					"app.kubernetes.io/instance":  "default",
					"app.kubernetes.io/component": "dbclaim",
				},
			},
			Spec: v1alpha1.DBInstanceSpec{
				ForProvider: v1alpha1.DBInstanceParameters{
					DBInstanceClass: ptr.String("db.t2.micro"),
					Engine:          ptr.String("postgres"),
				},
			},
		}
		Expect(k8sClient.Create(context.Background(), dbInstance)).To(Succeed())

		// Create a corresponding DatabaseClaim resource
		dbClaim = &persistancev1.DatabaseClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "default-dbclaim",
			},
			Spec: persistancev1.DatabaseClaimSpec{
				DatabaseName: "sample-app",
				SecretName:   "default-secret",
			},
		}
		Expect(k8sClient.Create(context.Background(), dbClaim)).To(Succeed())
	})

	// Clean up resources after each test case
	AfterEach(func() {

		// Delete the DBInstance resource
		Expect(k8sClient.Delete(context.Background(), dbInstance)).To(Succeed())
		// Delete the DatabaseClaim resource
		Expect(k8sClient.Delete(context.Background(), dbClaim)).To(Succeed())
	})

	// Test case: Reconcile should update DatabaseClaim status to Synced=True
	It("Should reconcile and update the DatabaseClaim status to Synced=True", func() {

		// Retrieve the created DBInstance and update its status
		updatedInstance := &v1alpha1.DBInstance{}
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{
			Namespace: "default", Name: "default-dbinstance"}, updatedInstance)).To(Succeed())

		// Set the DBInstance conditions to indicate it is synced and ready
		updatedInstance.Status.Conditions = []v1.Condition{
			{
				Type:               "Synced",
				Status:             corev1.ConditionTrue,
				Reason:             "Available",
				Message:            "DBInstance is synced",
				LastTransitionTime: metav1.Now(),
			},
			{
				Type:               "Ready",
				Status:             corev1.ConditionTrue,
				Reason:             "Available",
				Message:            "DBInstance is ready",
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(k8sClient.Status().Update(context.Background(), updatedInstance)).To(Succeed())

		// Trigger the reconcile loop
		_, err := statusControllerReconciler.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: "default", Name: "default-dbinstance"},
		})
		Expect(err).NotTo(HaveOccurred())

		// Retrieve the updated DatabaseClaim and check its status
		updatedClaim := &persistancev1.DatabaseClaim{}
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{
			Namespace: "default", Name: "default-dbclaim"}, updatedClaim)).To(Succeed())

		// Print the updated DatabaseClaim conditions for debugging
		fmt.Printf("updatedClaim: %v\n", updatedClaim.Status.Conditions)

		// Verify that the Synced condition is set to True
		condition := FindCondition(updatedClaim.Status.Conditions, "Synced")
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal("Available"))
		Expect(condition.Message).To(Equal("Database is provisioned."))
	})

	// Test case: Reconcile should update DatabaseClaim status to Synced=False
	It("Should update DatabaseClaim status to Synced=False", func() {

		// Retrieve the created DBInstance and simulate a failed sync condition
		updatedInstance := &v1alpha1.DBInstance{}
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "default-dbinstance"}, updatedInstance)).To(Succeed())
		updatedInstance.Status.Conditions = []v1.Condition{
			{
				Type:               "Synced",
				Status:             corev1.ConditionFalse,
				Reason:             "SyncFailed",
				Message:            "DBInstance synchronization failed",
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(k8sClient.Status().Update(context.Background(), updatedInstance)).To(Succeed())

		// Trigger the reconcile loop
		_, err := statusControllerReconciler.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: "default", Name: "default-dbinstance"},
		})
		Expect(err).NotTo(HaveOccurred())

		// Retrieve the updated DatabaseClaim and check its status
		updatedClaim := &persistancev1.DatabaseClaim{}
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "default-dbclaim"}, updatedClaim)).To(Succeed())

		// Verify that the Synced condition is set to False
		condition := FindCondition(updatedClaim.Status.Conditions, "Synced")
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("Unavailable"))
		Expect(condition.Message).To(Equal("Reconciliation encountered an issue: SyncFailed: DBInstance synchronization failed"))
	})
})

// Helper function to find a condition by type in a list of conditions
func FindCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for _, cond := range conditions {
		if cond.Type == condType {
			return &cond
		}
	}
	// Fail the test if the condition is not found
	Fail(fmt.Sprintf("Condition %s not found", condType))
	return nil
}
