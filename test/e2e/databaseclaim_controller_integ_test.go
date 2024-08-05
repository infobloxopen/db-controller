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

var _ = Describe("db-controller", func() {

	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		BDClaimName = "test-dbclaim"
	)

	Context("When updating DB Claim Status", func() {
		It("Should update DB Claim status", func() {
			By("By creating a new DB Claim")
			ctx := context.Background()
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
					Class:                 ptr.To(""),
					AppID:                 "sample-app",
					DatabaseName:          "sample_app",
					InstanceLabel:         "sample-connection",
					SecretName:            "sample-secret",
					Username:              "sample_user",
					EnableSuperUser:       ptr.To(false),
					EnableReplicationRole: ptr.To(false),
					UseExistingSource:     ptr.To(false),
				},
			}
			Expect(k8sClient.Create(ctx, dbClaim)).Should(Succeed())

			Eventually(func() bool {
				dbClaim := &persistancev1.DatabaseClaim{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: BDClaimName, Namespace: namespace}, dbClaim)
				if err != nil {
					return false
				}

				return dbClaim.Status.Error == "can't find any instance label matching fragment keys"

			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, dbClaim)).Should(Succeed())

		})
	})
})
