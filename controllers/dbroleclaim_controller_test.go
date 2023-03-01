package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// +kubebuilder:docs-gen:collapse=Imports

var _ = Describe("dbRoleClaim controller", func() {
	const (
		DBClaimName      = "test-dbclaim-1"
		RoleClaimName    = "test-dbroleclaim"
		defaultNamespace = "default"
		RoleClaimSecret  = "test-dbroleclaim-secret"
		DBClaimSecret    = "teste-dbclaim-secret"
		URIDSN           = "uri-dsn"
		DSN              = "cG9zdGdyZXM6Ly91c2VyX2E6dGVzdHBhc3N3b3JkQHRlc3Rob3N0LnRlc3QudXMtZWFzdC0xLnJkcy5hbWF6b25hd3MuY29tOjU0MzIvdGVzdGRhdGFiYXNlP3NzbG1vZGU9cmVxdWlyZQ=="

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)
	Class := "donotreconcile"
	Context("When creating dbRoleClaim", func() {
		It("Should eventually create a secret", func() {
			By("creating a new dbRoleClaim with a dbclaim status error")
			ctx := context.Background()
			dbRoleClaim := &persistancev1.DbRoleClaim{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "persistance.atlas.infoblox.com/v1",
					Kind:       "DbRoleClaim",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      RoleClaimName,
					Namespace: defaultNamespace,
				},
				Spec: persistancev1.DbRoleClaimSpec{
					SourceDatabaseClaim: &persistancev1.SourceDatabaseClaim{
						Name:      DBClaimName,
						Namespace: defaultNamespace,
					},
					SecretName: RoleClaimSecret,
				},
			}
			Expect(k8sClient.Create(ctx, dbRoleClaim)).Should(Succeed())
			roleClaimLookupKey := types.NamespacedName{Name: RoleClaimName, Namespace: defaultNamespace}
			createdDbRoleClaim := &persistancev1.DbRoleClaim{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, roleClaimLookupKey, createdDbRoleClaim)
				return err == nil
			}, timeout, interval).Should(BeTrue())
			Expect(createdDbRoleClaim.Status.MatchedSourceClaim).Should(Equal(""))
			Eventually(func() (string, error) {
				err := k8sClient.Get(ctx, roleClaimLookupKey, createdDbRoleClaim)
				if err != nil {
					return "", err
				}
				return createdDbRoleClaim.Status.Error, nil
			}, timeout, interval).Should(Equal(DBClaimName + " dbclaim not found"))
			By("updating dbRoleClaim with a secret status error")
			dbClaim := &persistancev1.DatabaseClaim{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "persistance.atlas.infoblox.com/v1",
					Kind:       "DatabaseClaim",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      DBClaimName,
					Namespace: defaultNamespace,
				},
				Spec: persistancev1.DatabaseClaimSpec{
					Class:        &Class,
					SecretName:   DBClaimSecret,
					Type:         "postgres",
					Username:     "sample_user",
					DatabaseName: "sampledb",
					DSNName:      "postgres-dsn",
				},
			}
			Expect(k8sClient.Create(ctx, dbClaim)).Should(Succeed())
			Eventually(func() (string, error) {
				err := k8sClient.Get(ctx, roleClaimLookupKey, createdDbRoleClaim)
				if err != nil {
					return "", err
				}
				return createdDbRoleClaim.Status.Error, nil
			}, timeout, interval).Should(Equal(DBClaimSecret + " source secret not found"))
			Eventually(func() (string, error) {
				err := k8sClient.Get(ctx, roleClaimLookupKey, createdDbRoleClaim)
				if err != nil {
					return "", err
				}
				return createdDbRoleClaim.Status.MatchedSourceClaim, nil
			}, timeout, interval).Should(Equal(defaultNamespace + "/" + DBClaimName))
			By("creating a new secret")
			roleSecret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Secret",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      DBClaimSecret,
					Namespace: defaultNamespace,
				},
				Type: "Opaque",
				Data: map[string][]byte{
					"database": []byte("dGVzdGRhdGFiYXNl"),
					"dsn.txt":  []byte("aG9zdD10ZXN0aG9zdC50ZXN0LnVzLWVhc3QtMS5yZHMuYW1hem9uYXdzLmNvbSBwb3J0PTU0MzIgdXNlcj11c2VyX2EgcGFzc3dvcmQ9dGVzdHBhc3N3b3JkIGRibmFtZT10ZXN0ZGF0YWJhc2Ugc3NsbW9kZT1yZXF1aXJl"),
					"hostname": []byte("dGVzdGhvc3QudGVzdC51cy1lYXN0LTEucmRzLmFtYXpvbmF3cy5jb20="),
					"password": []byte("dGVzdHBhc3N3b3Jk"),
					"port":     []byte("NTQzMg=="),
					"sslmode":  []byte("cmVxdWlyZQ=="),
					URIDSN:     []byte(DSN),
					"username": []byte("dXNlcl9h"),
				},
			}
			Expect(k8sClient.Create(ctx, roleSecret)).Should(Succeed())
			Eventually(func() (string, error) {
				err := k8sClient.Get(ctx, roleClaimLookupKey, createdDbRoleClaim)
				if err != nil {
					return "", err
				}
				return createdDbRoleClaim.Status.Error, nil
			}, timeout, interval).Should(Equal(""))
			Eventually(func() (string, error) {
				err := k8sClient.Get(ctx, roleClaimLookupKey, createdDbRoleClaim)
				if err != nil {
					return "", err
				}
				return createdDbRoleClaim.Status.SourceSecret, nil
			}, timeout, interval).Should(Equal(defaultNamespace + "/" + DBClaimSecret))
			roleClaimSecretKey := types.NamespacedName{Name: RoleClaimSecret, Namespace: defaultNamespace}
			createdDbRoleClaimSecret := &corev1.Secret{}
			Eventually(func() (string, error) {
				err := k8sClient.Get(ctx, roleClaimSecretKey, createdDbRoleClaimSecret)
				if err != nil {
					return "", err
				}
				return string(createdDbRoleClaimSecret.Data[URIDSN]), nil
			}, timeout, interval).Should(Equal(DSN))

		})
	})
})
