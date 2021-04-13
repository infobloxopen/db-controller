package controllers

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
)

var _ = Describe("db-controller", func() {

	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		BDClaimName      = "test-dbclaim"
		BDClaimNamespace = "default"
	)

	Context("When updating DB Claim Status", func() {
		It("Should update DB Claim status", func() {
			By("By creating a new DB Claim")
			ctx := context.Background()
			dbClaim := &persistancev1.DatabaseClaim{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "persistance.atlas.infoblox.com/v1",
					Kind:       "DatabaseClaim",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      BDClaimName,
					Namespace: BDClaimNamespace,
				},
				Spec: persistancev1.DatabaseClaimSpec{
					AppID:         "sample-app",
					DatabaseName:  "sample_app",
					InstanceLabel: "sample-connection",
					SecretName:    "sample-secret",
					Username:      "sample_user",
				},
			}
			Expect(k8sClient.Create(ctx, dbClaim)).Should(Succeed())
		})
	})
})
