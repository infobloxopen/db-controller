package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
)

//var hasOperationalTag = databaseclaim.HasOperationalTag

var _ = Describe("invalid-claim", func() {

	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		BDClaimName = "simple-dbclaim"
	)
	ctx := context.Background()

	BeforeEach(func() {

		By("By creating a new DB Claim")
		Expect(namespace).NotTo(Equal(""), "you must set the namespace")
		dbClaim := &persistancev1.DatabaseClaim{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "persistance.atlas.infoblox.com/v1",
				Kind:       "DatabaseClaim",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      BDClaimName,
				Namespace: namespace,
			},
			Spec: persistancev1.DatabaseClaimSpec{
				Class:                 ptr.To(namespace),
				AppID:                 "sample-app",
				DatabaseName:          "sample_app",
				SecretName:            "sample-secret",
				Username:              "sample_user",
				EnableSuperUser:       ptr.To(false),
				EnableReplicationRole: ptr.To(false),
				UseExistingSource:     ptr.To(false),
				Type:                  "invalid",
			},
		}
		Expect(k8sClient.Create(ctx, dbClaim)).Should(Succeed())

	})

	AfterEach(func() {
		By("Ensuring that the DB Claim is deleted")
		ctx := context.Background()
		dbClaim := &persistancev1.DatabaseClaim{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: BDClaimName, Namespace: namespace}, dbClaim)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, dbClaim)).Should(Succeed())
	})

	Context("When updating DB Claim Status", func() {
		It("Should update DB Claim status", func() {

			dbClaim := &persistancev1.DatabaseClaim{}
			Eventually(func() string {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: BDClaimName, Namespace: namespace}, dbClaim)
				if err != nil {
					return err.Error()
				}
				return dbClaim.Status.Error

			}, 20*time.Second, 250*time.Millisecond).Should(HavePrefix("invalid database type: \"invalid\" must be one of"))
		})
	})
})
