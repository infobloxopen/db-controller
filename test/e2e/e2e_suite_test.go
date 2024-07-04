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
	"fmt"
	"os"
	"os/exec"
	"testing"

	crossplanerdsv1alpha1 "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/test/utils"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	namespace string
	k8sClient client.Client
)

func init() {
	namespace = os.Getenv("NAMESPACE")
}

// FIXME: remove this and use namespace instead
var class = ""

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	fmt.Fprintf(GinkgoWriter, "Starting migration suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {

	Expect(namespace).NotTo(Equal(""), "you must set the namespace")
	class = namespace

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	var err error

	// Add all the schemas needed
	err = persistancev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = crossplanerdsv1alpha1.SchemeBuilder.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(config.GetConfigOrDie(), client.Options{})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	By("Pushing the operator images")
	cmd := exec.Command("make", "push-images")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	By("Helm upgrading the manager")
	cmd = exec.Command("make", "deploy")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

})

var _ = AfterSuite(func() {

	By("Helm upgrading the manager")
	cmd := exec.Command("make", "undeploy")
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())
})
