package controller_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/internal/controller"
	"github.com/infobloxopen/db-controller/pkg/databaseclaim"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("DBInstanceStatusReconciler", func() {
	var (
		dbInstance           *v1alpha1.DBInstance
		dbClaim              *persistancev1.DatabaseClaim
		controllerReconciler *controller.DBInstanceStatusReconciler
		k8sClient            client.Client
	)

	BeforeEach(func() {
		controllerReconciler = &controller.DBInstanceStatusReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			StatusManager: databaseclaim.NewStatusManager(k8sClient, nil),
		}

		// Ensure DBInstance is clean before creating
		existingInstance := &v1alpha1.DBInstance{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "test-dbinstance"}, existingInstance)
		if err == nil {
			Expect(k8sClient.Delete(context.Background(), existingInstance)).To(Succeed())
		}

		dbInstance = &v1alpha1.DBInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dbinstance",
				Labels: map[string]string{
					"app.kubernetes.io/instance":  "test-dbclaim",
					"app.kubernetes.io/component": "database",
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
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "test-dbclaim"}, existingClaim)
		if err == nil {
			Expect(k8sClient.Delete(context.Background(), existingClaim)).To(Succeed())
		}

		dbClaim = &persistancev1.DatabaseClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dbclaim",
			},
			Spec: persistancev1.DatabaseClaimSpec{
				DatabaseName: "sample-app",
				SecretName:   "test-secret",
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
		_, err := controllerReconciler.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-dbinstance"},
		})
		Expect(err).NotTo(HaveOccurred())

		updatedClaim := &persistancev1.DatabaseClaim{}
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "test-dbclaim"}, updatedClaim)).To(Succeed())

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
