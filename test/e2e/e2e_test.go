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
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/infobloxopen/db-controller/test/utils"
)

var _ = Describe("controller", Ordered, func() {
	// BeforeAll(func() {
	// 	By("installing prometheus operator")
	// 	Expect(utils.InstallPrometheusOperator()).To(Succeed())

	// 	By("installing the cert-manager")
	// 	Expect(utils.InstallCertManager()).To(Succeed())

	// 	By("creating manager namespace")
	// 	cmd := exec.Command("kubectl", "create", "ns", namespace)
	// 	_, _ = utils.Run(cmd)
	// })

	// AfterAll(func() {
	// 	By("uninstalling the Prometheus manager bundle")
	// 	utils.UninstallPrometheusOperator()

	// 	By("uninstalling the cert-manager bundle")
	// 	utils.UninstallCertManager()

	// 	By("removing manager namespace")
	// 	cmd := exec.Command("kubectl", "delete", "ns", namespace)
	// 	_, _ = utils.Run(cmd)
	// })

	Context("Operator", func() {
		It("should run successfully", func() {
			var err error

			// Only a specific set of clusters are allowed here to ensure you know what you are doing

			// By("installing CRDs")
			// cmd = exec.Command("make", "install")
			// _, err = utils.Run(cmd)
			// ExpectWithOffset(1, err).NotTo(HaveOccurred())

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
	})
})
