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
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	. "github.com/infobloxopen/db-controller/pkg/dbclient"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	namespace string
	k8sClient client.Client
	TestDb    *TestDB
)

func init() {
	namespace = os.Getenv("NAMESPACE")
}

// FIXME: remove this and use namespace instead
var class = ""

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	fmt.Fprintf(GinkgoWriter, "Starting E2E suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {

	Expect(namespace).NotTo(Equal(""), "you must set the namespace")
	class = namespace

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	//var err error

	// // Add all the schemas needed
	// err = persistancev1.AddToScheme(scheme.Scheme)
	// Expect(err).NotTo(HaveOccurred())

	// err = crossplanerdsv1alpha1.SchemeBuilder.AddToScheme(scheme.Scheme)
	// Expect(err).NotTo(HaveOccurred())

	// k8sClient, err = client.New(config.GetConfigOrDie(), client.Options{})
	// Expect(err).NotTo(HaveOccurred())
	// Expect(k8sClient).NotTo(BeNil())

	// By("Building image")
	// cmd := exec.Command("make", "build-images")
	// _, err = utils.Run(cmd)
	// ExpectWithOffset(1, err).NotTo(HaveOccurred())

	// By("Pushing the operator images")
	// cmd = exec.Command("make", "push-images")
	// _, err = utils.Run(cmd)
	// ExpectWithOffset(1, err).NotTo(HaveOccurred())

	// By("Helm upgrading the manager")
	// cmd = exec.Command("make", "deploy")
	// _, err = utils.Run(cmd)
	// ExpectWithOffset(1, err).NotTo(HaveOccurred())

	TestDb, _ = SetupSqlDB("mainUser", "masterpassword")

})

var _ = AfterSuite(func() {

	// By("Helm upgrading the manager")
	// cmd := exec.Command("make", "undeploy")
	// _, err := utils.Run(cmd)
	// Expect(err).NotTo(HaveOccurred())

	TestDb.Close()
})
