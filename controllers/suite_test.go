/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment

var controllerConfig = []byte(`
    authSource: "secret"
    passwordConfig:
      passwordComplexity: enabled
      minPasswordLength: "15"
      passwordRotationPeriod: "60"
    sample-connection:
      username: postgres
      host: localhost
      port: 5432
      useSSL: false
      passwordSecretRef: sample-master-secret
    another.connection:
      username: root
      host: some.other.service
      port: 5412
      useSSL: false
      passwordSecretRef: another-connection-secret
`)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	const (
		MasterSecretName = "sample-master-secret"
		Namespace        = "default"
		timeout          = time.Second * 10
		interval         = time.Millisecond * 250
	)
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = persistancev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	ctx := context.Background()
	masterSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Namespace,
			Name:      MasterSecretName,
		},
		Type: "Opaque",
		Data: map[string][]byte{
			"Password": []byte("postgres"),
		},
	}
	Expect(k8sClient.Create(ctx, masterSecret)).Should(Succeed())

	secretLookupKey := types.NamespacedName{Name: MasterSecretName, Namespace: Namespace}
	createdSecret := &corev1.Secret{}

	err = k8sClient.Get(ctx, secretLookupKey, createdSecret)
	Expect(err).NotTo(HaveOccurred())
	Expect(createdSecret.Name).Should(Equal(MasterSecretName))

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&DatabaseClaimReconciler{
		Client: k8sClient,
		Scheme: k8sManager.GetScheme(),
		Log:    ctrl.Log.WithName("controllers").WithName("DB-controller"),
		Config: NewConfig(controllerConfig),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

	time.Sleep(10 * time.Second)
	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	//  commented that line due to controller-runtime issue: https://github.com/kubernetes-sigs/controller-runtime/issues/1571 Should be reverted later
	//	Expect(err).ToNot(HaveOccurred())
	if err.Error() != "timeout waiting for process kube-apiserver to stop" {
		Expect(err).ToNot(HaveOccurred())
	} else {
		By(err.Error())
	}
})
