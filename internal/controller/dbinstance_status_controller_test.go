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

	BeforeEach(func() {
		// Ensure DBInstance is clean before creating
		existingInstance := &v1alpha1.DBInstance{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "default-dbinstance"}, existingInstance)
		if err == nil {
			Expect(k8sClient.Delete(context.Background(), existingInstance)).To(Succeed())
		}

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
			Status: v1alpha1.DBInstanceStatus{
				ResourceStatus: v1.ResourceStatus{
					ConditionedStatus: v1.ConditionedStatus{
						Conditions: []v1.Condition{
							{
								Type:    "Ready",
								Status:  corev1.ConditionTrue,
								Reason:  "Available",
								Message: "DBInstance is ready",
							},
							{
								Type:    "Synced",
								Status:  corev1.ConditionTrue,
								Reason:  "Available",
								Message: "DBInstance is synced",
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(context.Background(), dbInstance)).To(Succeed())

		// Ensure DatabaseClaim is clean before creating
		existingClaim := &persistancev1.DatabaseClaim{}
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "default-dbclaim"}, existingClaim)
		if err == nil {
			Expect(k8sClient.Delete(context.Background(), existingClaim)).To(Succeed())
		}

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

	AfterEach(func() {
		if dbInstance != nil {
			Expect(k8sClient.Delete(context.Background(), dbInstance)).To(Succeed())
		}
		if dbClaim != nil {
			Expect(k8sClient.Delete(context.Background(), dbClaim)).To(Succeed())
		}
	})

	It("Should reconcile and update the DatabaseClaim status", func() {
		_, err := statusControllerReconciler.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: "default", Name: "default-dbinstance"},
		})
		Expect(err).NotTo(HaveOccurred())

		updatedClaim := &persistancev1.DatabaseClaim{}
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "default-dbclaim"}, updatedClaim)).To(Succeed())

		fmt.Printf("updatedClaim: %v\n", updatedClaim.Status.Conditions)

		condition := FindCondition(updatedClaim.Status.Conditions, "Synced")
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal("Available"))
		Expect(condition.Message).To(Equal("DBInstance is synced"))
	})
})

func FindCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for _, cond := range conditions {
		if cond.Type == condType {
			return &cond
		}
	}
	Fail(fmt.Sprintf("Condition %s not found", condType))
	return nil
}
