/*
Copyright 2024.

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

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/test/utils"
	crossplanegcpv1beta2 "github.com/upbound/provider-gcp/apis/alloydb/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	cloud     string
	env       string
	namespace string
	k8sClient client.Client
)

func init() {
	namespace = os.Getenv("NAMESPACE")
}

// FIXME: remove this and use namespace instead
var class = ""
var logger logr.Logger

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	fmt.Fprintf(GinkgoWriter, "Starting E2E suite\n")
	RunSpecs(t, "e2e suite", Label("FailFast"))
	logger = NewGinkgoLogger(t)
}

func NewGinkgoLogger(t *testing.T) logr.Logger {
	// Create a new logger with the formatter and a test writer
	return funcr.New(func(prefix, args string) {
		t.Log(prefix, args)
	}, funcr.Options{
		Verbosity: 1,
	})
}

var _ = BeforeSuite(func() {
	Expect(namespace).NotTo(Equal(""), "you must set the namespace")
	class = namespace

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	var err error

	// Add all the schemas needed
	err = v1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = crossplaneaws.SchemeBuilder.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = crossplanegcpv1beta2.SchemeBuilder.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(config.GetConfigOrDie(), client.Options{})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// read kubectl context from the k8sClient
	env, err = utils.GetKubeContext()
	Expect(env).NotTo(BeEmpty())
	Expect(err).NotTo(HaveOccurred())

	// Check if current context is box-3, kind or gcp-ddi-dev-use1
	Expect(env).To(BeElementOf("box-3", "kind", "gcp-ddi-dev-use1"), "This test can only run in box-3, kind or gcp-ddi-dev-use1")

	switch {
	case env == "box-3":
		fallthrough
	case env == "kind":
		cloud = "aws"
	case env == "gcp-ddi-dev-use1":
		cloud = "gcp"
	default:
		cloud = "invalid"
	}

	if len(os.Getenv("NODEPLOY")) > 0 {
		logger.Info("Skipping deployment")
		return
	}

	By("Building image")
	cmd := exec.Command("make", "build-images")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	By("Pushing the operator images")
	cmd = exec.Command("make", "push-images")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	By("Helm upgrading the manager")
	cmd = exec.Command("make", "deploy")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	By("validating that the controller-manager pod is running as expected")
	verifyControllerUp := func() error {
		// Get pod name

		cmd := exec.Command("make", "helm-test")
		_, err = utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())

		return nil
	}
	EventuallyWithOffset(1, verifyControllerUp, time.Minute, time.Second).Should(Succeed())

})

var _ = AfterSuite(func() {

	if os.Getenv("NOCLEANUP") != "" {
		return
	}
	By("Cleaning up resources")

	ctx := context.Background()
	// delete db1 if it exists
	claim := &v1.DatabaseClaim{}
	for _, db := range []string{db1, db2, namespace + "-dbproxy-test"} {
		nname := types.NamespacedName{
			Name:      db,
			Namespace: namespace,
		}
		
		By("Checking DatabaseClaim: " + db)
		if err := k8sClient.Get(ctx, nname, claim); err == nil {
			if _, exists := claim.GetLabels()["persistance.atlas.infoblox.com/allow-deletion"]; !exists {
				By("Setting allow-deletion label for DatabaseClaim: " + db)
				claim.SetLabels(map[string]string{
					"persistance.atlas.infoblox.com/allow-deletion": "enabled",
				})
				Expect(k8sClient.Update(ctx, claim)).Should(Succeed())
			}
	
			By("Deleting DatabaseClaim: " + db)
			Expect(k8sClient.Delete(ctx, claim)).Should(Succeed())
			
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, nname, claim))
			}).Should(BeTrue())
		}
	}
	

	inst := crossplaneaws.DBInstance{}
	for _, db := range []string{dbinstance1, dbinstance1update} {
		nname := types.NamespacedName{
			Name:      db,
			Namespace: namespace,
		}

		By("Checking DBInstance: " + db)

		if err := k8sClient.Get(ctx, nname, &inst); err == nil {
			By("Deleting DBInstance: " + db)
			Expect(k8sClient.Delete(ctx, &inst)).Should(Succeed())

			// CR is stuck in deletion, force a deletion by removing the finalizer
			err := k8sClient.Get(ctx, nname, &inst)
			if errors.IsNotFound(err) {
				By("DBInstance is stuck in deletion; removing finalizer for DBInstance: " + db)

				// patch the finalizer to prevent crossplane from blocking the deletion
				inst.SetFinalizers([]string{})
				Expect(k8sClient.Update(ctx, &inst)).Should(Succeed())

				Expect(k8sClient.Delete(ctx, &inst)).Should(Succeed())

				Eventually(func() bool {
					return errors.IsNotFound(k8sClient.Get(ctx, nname, &inst))
				}, timeoutE2e, intervalE2e).Should(Succeed())
				By("DBInstance successfully deleted: " + db)
			}
		}
	}

	// Removes the dbinstance with the 15.3 version error.
	nname := types.NamespacedName{
		Name:      rds1,
		Namespace: namespace,
	}
	By("Checking DBInstance: " + rds1)
	if err := k8sClient.Get(ctx, nname, &inst); err == nil {
		By("Deleting DBInstance: " + rds1)
		Expect(k8sClient.Delete(ctx, &inst)).Should(Succeed())
	}

	secrets := []string{dbinstance1, fmt.Sprintf("%s-master", dbinstance1)}
	for _, secret := range secrets {
		nname := types.NamespacedName{
			Name:      secret,
			Namespace: namespace,
		}
		sec := &corev1.Secret{}
		if err := k8sClient.Get(ctx, nname, sec); err == nil {
			By("Deleting Secret       : " + secret)
			Expect(k8sClient.Delete(ctx, sec)).Should(Succeed())
		}
	}

})
