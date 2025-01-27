package controller

import (
	"context"
	"fmt"

	"github.com/aws/smithy-go/ptr"
	"github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("DBInstanceStatusReconciler", func() {
	var (
		dbInstance *v1alpha1.DBInstance
		dbClaim    *persistancev1.DatabaseClaim
	)

	BeforeEach(func() {
		// Create a DBInstance resource with required labels.
		dbInstance = &v1alpha1.DBInstance{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "default-dbinstance",
				Labels: map[string]string{
					"app.kubernetes.io/dbclaim-name":      "default-dbclaim",
					"app.kubernetes.io/dbclaim-namespace": "default",
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

		// Create a corresponding DatabaseClaim resource.
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

	// Clean up resources after each test case.
	AfterEach(func() {
		Expect(k8sClient.Delete(context.Background(), dbInstance)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), dbClaim)).To(Succeed())
	})

	It("Should reconcile and update the DatabaseClaim status to Synced=True", func() {
		// Retrieve the created DBInstance and update its status
		var dbInstanceObjKey = types.NamespacedName{
			Namespace: "default",
			Name:      "default-dbinstance",
		}
		var updatedInstance v1alpha1.DBInstance
		Expect(k8sClient.Get(context.Background(), dbInstanceObjKey, &updatedInstance)).To(Succeed())

		// Set the DBInstance conditions to indicate it is synced and ready.
		updatedInstance.Status.Conditions = []v1.Condition{
			{
				Type:               v1.TypeSynced,
				Status:             corev1.ConditionTrue,
				Reason:             v1.ReasonAvailable,
				Message:            "DBInstance is synced",
				LastTransitionTime: metav1.Now(),
			},
			{
				Type:               v1.TypeReady,
				Status:             corev1.ConditionTrue,
				Reason:             v1.ReasonAvailable,
				Message:            "DBInstance is ready",
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(k8sClient.Status().Update(context.Background(), &updatedInstance)).To(Succeed())

		// Trigger the reconcile loop.
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "default",
				Name:      "default-dbinstance",
			},
		}
		_, err := statusControllerReconciler.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())

		// Retrieve the updated DatabaseClaim and check its status.
		dbClaimObjKey := types.NamespacedName{
			Namespace: "default",
			Name:      "default-dbclaim",
		}
		var updatedClaim persistancev1.DatabaseClaim
		Expect(k8sClient.Get(context.Background(), dbClaimObjKey, &updatedClaim)).To(Succeed())

		// Verify that the Synced condition is set to True.
		condition := FindCondition(updatedClaim.Status.Conditions, "Synced")
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal("Available"))
		Expect(condition.Message).To(Equal("Database is provisioned."))
	})

	It("Should update DatabaseClaim status to Synced=False", func() {
		var dbInstanceObjKey = types.NamespacedName{
			Namespace: "default",
			Name:      "default-dbinstance",
		}
		var updatedInstance v1alpha1.DBInstance
		Expect(k8sClient.Get(context.Background(), dbInstanceObjKey, &updatedInstance)).To(Succeed())

		updatedInstance.Status.Conditions = []v1.Condition{
			{
				Type:               v1.TypeSynced,
				Status:             corev1.ConditionFalse,
				Reason:             "SyncFailed",
				Message:            "DBInstance synchronization failed",
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(k8sClient.Status().Update(context.Background(), &updatedInstance)).To(Succeed())

		// Trigger the reconcile loop.
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "default",
				Name:      "default-dbinstance",
			},
		}
		_, err := statusControllerReconciler.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())

		// Retrieve the updated DatabaseClaim and check its status.
		dbClaimObjKey := types.NamespacedName{
			Namespace: "default",
			Name:      "default-dbclaim",
		}
		var updatedClaim persistancev1.DatabaseClaim
		Expect(k8sClient.Get(context.Background(), dbClaimObjKey, &updatedClaim)).To(Succeed())

		// Verify that the Synced condition is set to False.
		condition := FindCondition(updatedClaim.Status.Conditions, "Synced")
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("Unavailable"))
		Expect(condition.Message).To(Equal("Reconciliation encountered an issue: SyncFailed: DBInstance synchronization failed"))
	})
})

// Helper function to find a condition by type in a list of conditions.
func FindCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for _, cond := range conditions {
		if cond.Type == condType {
			return &cond
		}
	}

	// Fail the test if the condition is not found.
	Fail(fmt.Sprintf("Condition %s not found", condType))
	return nil
}
