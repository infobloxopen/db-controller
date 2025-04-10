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

var _ = Describe("dsnexec defaulting", func() {
	BeforeEach(func() {

		By("create dependent resources")

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		resource := &v1.DatabaseClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-db",
				Namespace: "default",
				Labels:    map[string]string{"persistance.atlas.infoblox.com/allow-deletion": "enabled"},
			},
			Spec: v1.DatabaseClaimSpec{
				SecretName: "test",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "default-db"}, resource)
			return err == nil
		}).Should(BeTrue())

	})

	AfterEach(func() {
		resource := &v1.DatabaseClaim{}
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "default-db"}, resource)
		Expect(err).NotTo(HaveOccurred())

		By("Cleanup the specific resource instance")
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

		var secret corev1.Secret
		err = k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test"}, &secret)
		Expect(err).NotTo(HaveOccurred())

		By("Cleanup the specific resource core objects")
		Expect(k8sClient.Delete(ctx, &secret)).To(Succeed())

		var list corev1.PodList
		Expect(k8sClient.List(ctx, &list)).ToNot(HaveOccurred())
		for _, item := range list.Items {
			err := k8sClient.Delete(ctx, &item)
			Expect(err).ToNot(HaveOccurred())
		}

	})

	// TODO: change to table driven tests
	It("should successfully mutating a pod", func() {
		By("Check pod is mutated")

		name := "annotation-enabled"
		Expect(k8sClient.Create(ctx, makePodExec(name, "default-db"))).NotTo(HaveOccurred())
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations).To(HaveKeyWithValue(AnnotationInjectedExec, "true"))

	})

	It("should successfully skip mutation", func() {
		By("Check pod is mutated")

		name := "annotation-disabled"

		Expect(k8sClient.Create(ctx, makePodExec(name, ""))).NotTo(HaveOccurred())
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations).NotTo(HaveKeyWithValue(AnnotationInjectedExec, "true"))

		name = "annotation-false"

		Expect(k8sClient.Create(ctx, makePodExec(name, ""))).NotTo(HaveOccurred())
		err = k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations).NotTo(HaveKeyWithValue(AnnotationInjectedExec, "true"))
		Expect(pod.Spec.Volumes).To(HaveLen(0))
		Expect(pod.Spec.Containers).To(HaveLen(1))

	})

	It("check initial volume and sidecar pod mutation", func() {

		name := "annotation-enabled"
		Expect(k8sClient.Create(ctx, makePodExec(name, "default-db"))).NotTo(HaveOccurred())
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations).To(HaveKeyWithValue(AnnotationInjectedExec, "true"))
		By("Check secret volume")
		Expect(pod.Spec.Volumes).To(HaveLen(2))
		Expect(pod.Spec.Volumes[0].Name).To(Equal(VolumeNameExec))
		Expect(pod.Spec.Volumes[0].VolumeSource.Secret.SecretName).To(Equal("test"))
		Expect(pod.Spec.Volumes[0].VolumeSource.Secret.Optional).To(BeNil())
		Expect(pod.Spec.Volumes[1].Name).To(Equal(VolumeNameExecConfig))
		Expect(pod.Spec.Volumes[1].VolumeSource.Secret.SecretName).To(Equal("dsnexec-config-secret"))
		Expect(pod.Spec.Volumes[1].VolumeSource.Secret.Optional).To(BeNil())

		By("Check sidecar pod is injected")
		sidecar := pod.Spec.Containers[len(pod.Spec.Containers)-1]
		Expect(sidecar.Image).To(Equal(sidecarImageExec))
		Expect(sidecar.VolumeMounts).To(HaveLen(2))
		Expect(sidecar.VolumeMounts[0].Name).To(Equal(VolumeNameExec))
		Expect(sidecar.VolumeMounts[0].MountPath).To(Equal(MountPathExec))
		Expect(sidecar.VolumeMounts[0].ReadOnly).To(BeTrue())
		Expect(sidecar.ReadinessProbe).ToNot(BeNil())
		// FIXME: liveness probes deactivated DEVOPS-30642
		// Expect(sidecar.LivenessProbe).ToNot(BeNil())
	})

	It("pre-mutated pods are not re-mutated", func() {

		name := "annotation-enabled"
		Expect(k8sClient.Create(ctx, makeMutatedPodExec(name, "default-db", "test"))).NotTo(HaveOccurred())
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())
		By("Check pod has one set of volumes and sidecars")
		Expect(pod.Annotations).To(HaveKeyWithValue(AnnotationInjectedExec, "true"))
		Expect(pod.Spec.Volumes).To(HaveLen(2))
		Expect(pod.Spec.Containers).To(HaveLen(2))
	})

})

func makeMutatedPodExec(name, claimName, secretName string) *corev1.Pod {
	pod := makePodExec(name, claimName)
	Expect(mutatePodExec(context.TODO(), pod, secretName, sidecarImageExec, true, true)).To(Succeed())
	return pod

}

func makePodExec(name, claimName string) *corev1.Pod {
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

	switch name {
	case "annotation-enabled":
		pod.Labels = map[string]string{
			LabelCheckExec:  "enabled",
			LabelClaim:      claimName,
			LabelConfigExec: "dsnexec-config-secret",
			LabelClass:      "default",
		}
	case "annotation-disabled":
		pod.Labels = map[string]string{}
	case "annotation-false":
		pod.Labels = map[string]string{
			LabelCheckExec: "disabled",
		}
	}

	return pod
}
