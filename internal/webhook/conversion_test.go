package webhook

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1 "github.com/infobloxopen/db-controller/api/v1"
)

var _ = Describe("annotation conversions", func() {

	var (
		dbcName          = "default-db"
		dbcSecretName    = "dbc-default-db"
		configSecretName = "dsnexec-config"
		class            = "default"
	)

	BeforeEach(func() {

		By("create dependent resources")

		resource := &v1.DatabaseClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dbcName,
				Namespace: "default",
			},
			Spec: v1.DatabaseClaimSpec{
				SecretName: dbcSecretName,
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: dbcName}, resource)
			return err == nil
		}).Should(BeTrue())

		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dbcSecretName,
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "infoblox.com/v1",
						Kind:       "DatabaseClaim",
						Name:       dbcName,
						UID:        resource.UID,
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &secret)).To(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: dbcSecretName}, &secret)
			return err == nil
		}).Should(BeTrue())

		DeferCleanup(func() {
			By("Cleanup the databaseclaim")
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: dbcName}, resource)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Cleanup the databaseclaim secret")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: dbcSecretName}, &secret))
			Expect(k8sClient.Delete(ctx, &secret)).To(Succeed())
		})

	})

	AfterEach(func() {

		var list corev1.PodList
		Expect(k8sClient.List(ctx, &list)).ToNot(HaveOccurred())
		for _, item := range list.Items {
			err := k8sClient.Delete(ctx, &item)
			Expect(err).ToNot(HaveOccurred())
		}

	})

	It("should successfully skip mutation", func() {
		By("Check pod is not mutated")

		name := "deprecated-none"
		pod := makeDeprecatedPod(name, dbcSecretName, dbcName)
		Expect(k8sClient.Create(ctx, pod)).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)).NotTo(HaveOccurred())
		Expect(pod.Annotations).To(HaveLen(0))
	})

	It("should convert deprecated pod", func() {

		By("Check dbproxy pod is mutated")
		name := "deprecated-dbproxy"
		pod := makeConvertedPod(name, class, dbcSecretName, "")
		Expect(pod.Annotations).To(HaveKey(DeprecatedAnnotationMessages))
		Expect(pod.Labels).To(HaveKeyWithValue(LabelClaim, dbcName))
		Expect(pod.Labels).To(HaveKeyWithValue(LabelCheckProxy, "enabled"))
		Expect(pod.Labels).To(HaveKeyWithValue(LabelClass, class))

		By("Check dsnexec pod is converted")
		name = "deprecated-dsnexec"
		pod = makeConvertedPod(name, class, dbcSecretName, configSecretName)
		Expect(pod.Annotations).To(HaveKey(DeprecatedAnnotationMessages))
		Expect(pod.Labels).To(HaveKeyWithValue(LabelClaim, dbcName))
		Expect(pod.Labels).To(HaveKeyWithValue(LabelConfigExec, configSecretName))
		Expect(pod.Labels).To(HaveKeyWithValue(LabelCheckExec, "enabled"))
		Expect(pod.Labels).To(HaveKeyWithValue(LabelClass, class))

		By("Check dbproxy dsnexec combo pod is converted")
		name = "deprecated-both"
		pod = makeConvertedPod(name, class, dbcSecretName, configSecretName)
		Expect(pod.Annotations).To(HaveKey(DeprecatedAnnotationMessages))
		Expect(pod.Labels).To(HaveKeyWithValue(LabelClaim, dbcName))
		Expect(pod.Labels).To(HaveKeyWithValue(LabelConfigExec, configSecretName))
		Expect(pod.Labels).To(HaveKeyWithValue(LabelCheckProxy, "enabled"))
		Expect(pod.Labels).To(HaveKeyWithValue(LabelClass, class))

	})

	It("pre-converted pods are not re-mutated", func() {

		name := "deprecated-converted"
		pod := makeConvertedPod(name, class, dbcSecretName, "")
		delete(pod.Annotations, DeprecatedAnnotationMessages)
		Expect(k8sClient.Create(ctx, pod)).NotTo(HaveOccurred())

		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())
		By("Check pod does not have a deprecation message")
		Expect(pod.Annotations).NotTo(HaveKey(DeprecatedAnnotationMessages))
	})

	It("Deprecated pod is fully converted to have a dbproxy sidecar", func() {

		name := "deprecated-dbproxy"
		pod := makeDeprecatedPod(name, dbcSecretName, "")
		Expect(k8sClient.Create(ctx, pod)).NotTo(HaveOccurred())
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())

		By("Check pod is mutated")
		Expect(pod.Annotations).To(HaveKey(DeprecatedAnnotationMessages))
		Expect(pod.Annotations).To(HaveKeyWithValue(AnnotationInjectedProxy, "true"))
	})

})

func makeConvertedPod(name, class, secretName, configSecret string) *corev1.Pod {
	pod := makeDeprecatedPod(name, secretName, configSecret)
	Expect(convertPod(context.TODO(), k8sClient, class, pod)).To(Succeed())
	return pod
}

func makeDeprecatedPod(name, secretName, configSecret string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test",
					Image: "test",
				},
			},
		},
	}
	Expect(secretName).NotTo(BeEmpty())

	switch name {
	case "deprecated-dbproxy":
		pod.Annotations = map[string]string{
			DeprecatedAnnotationDBSecretPath: secretName,
		}
	case "deprecated-both":
		pod.Annotations = map[string]string{
			DeprecatedAnnotationDBSecretPath: secretName,
		}
		fallthrough
	case "deprecated-dsnexec":
		Expect(configSecret).NotTo(BeEmpty())
		pod.Annotations = map[string]string{
			DeprecatedAnnotationRemoteDBDSN:   secretName,
			DeprecatedAnnotationDSNExecConfig: configSecret,
		}
	case "deprecated-converted":
		pod.Annotations = map[string]string{
			DeprecatedAnnotationDBSecretPath: secretName,
		}
		pod.Labels = map[string]string{
			LabelConfigExec: "default-db",
		}
	case "deprecated-none":
	}

	return pod
}
