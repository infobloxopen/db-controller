package webhook

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	v1 "github.com/infobloxopen/db-controller/api/v1"
)

var _ = Describe("pod webhook defaulting", func() {
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
			},
			Spec: v1.DatabaseClaimSpec{
				Class:      ptr.To("default"),
				SecretName: "test",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

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
		Expect(k8sClient.Create(ctx, makePod(name, "default-db"))).NotTo(HaveOccurred())
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations).To(HaveKeyWithValue(AnnotationInjected, "true"))

	})

	It("should successfully skip mutation", func() {
		By("Check pod is mutated")

		name := "annotation-disabled"

		Expect(k8sClient.Create(ctx, makePod(name, ""))).NotTo(HaveOccurred())
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations).NotTo(HaveKeyWithValue(AnnotationInjected, "true"))

		name = "annotation-false"

		Expect(k8sClient.Create(ctx, makePod(name, ""))).NotTo(HaveOccurred())
		err = k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations).NotTo(HaveKeyWithValue(AnnotationInjected, "true"))
		Expect(pod.Spec.Volumes).To(HaveLen(0))
		Expect(pod.Spec.Containers).To(HaveLen(1))

	})

	It("check initial volume and sidecar pod mutation", func() {

		name := "annotation-enabled"
		Expect(k8sClient.Create(ctx, makePod(name, "default-db"))).NotTo(HaveOccurred())
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations).To(HaveKeyWithValue(AnnotationInjected, "true"))
		By("Check secret volume")
		Expect(pod.Spec.Volumes).To(HaveLen(1))
		Expect(pod.Spec.Volumes[0].Name).To(Equal(VolumeName))
		Expect(pod.Spec.Volumes[0].VolumeSource.Secret.SecretName).To(Equal("test"))
		Expect(pod.Spec.Volumes[0].VolumeSource.Secret.Optional).To(BeNil())

		By("Check sidecar pod is injected")
		sidecar := pod.Spec.Containers[len(pod.Spec.Containers)-1]
		Expect(sidecar.Image).To(Equal(sidecarImage))
		Expect(sidecar.VolumeMounts).To(HaveLen(1))
		Expect(sidecar.VolumeMounts[0].Name).To(Equal(VolumeName))
		Expect(sidecar.VolumeMounts[0].MountPath).To(Equal(MountPath))
		Expect(sidecar.VolumeMounts[0].ReadOnly).To(BeTrue())
	})

	It("pre-mutated pods are not re-mutated", func() {

		name := "annotation-enabled"
		Expect(k8sClient.Create(ctx, makeMutatedPod(name, "default-db", "test"))).NotTo(HaveOccurred())
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, pod)
		Expect(err).NotTo(HaveOccurred())
		By("Check pod has one set of volumes and sidecars")
		Expect(pod.Annotations).To(HaveKeyWithValue(AnnotationInjected, "true"))
		Expect(pod.Spec.Volumes).To(HaveLen(1))
		Expect(pod.Spec.Containers).To(HaveLen(2))
	})

})

func makeMutatedPod(name, claimName, secretName string) *corev1.Pod {
	pod := makePod(name, claimName)
	Expect(mutatePod(context.TODO(), pod, secretName, sidecarImage)).To(Succeed())
	return pod

}

func makePod(name, claimName string) *corev1.Pod {
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
			LabelCheck: claimName,
		}
	case "annotation-disabled":
		pod.Labels = map[string]string{}
	case "annotation-false":
		pod.Labels = map[string]string{
			LabelCheck: "",
		}
	}

	return pod
}
