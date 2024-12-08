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

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
)

var _ = Describe("DbRoleClaim Webhook", func() {
	var (
		obj       *persistancev1.DbRoleClaim
		oldObj    *persistancev1.DbRoleClaim
		validator DbRoleClaimCustomValidator
	)

	BeforeEach(func() {
		obj = &persistancev1.DbRoleClaim{}
		oldObj = &persistancev1.DbRoleClaim{}
		validator = DbRoleClaimCustomValidator{}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
	})

	Context("When deleting a DatabaseClaim under Validating Webhook", func() {
		It("Should allow deletion if the deletion override label is set to enabled", func() {
			By("Setting the deletion override label")
			obj.SetLabels(map[string]string{deletionOverrideLabel: "enabled"})

			By("Calling ValidateDelete")
			warnings, err := validator.ValidateDelete(ctx, obj)

			By("Expecting no errors and no warnings")
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("Should deny deletion if the deletion override label is not set", func() {
			By("Not setting the deletion override label")
			obj.SetLabels(map[string]string{})

			By("Calling ValidateDelete")
			warnings, err := validator.ValidateDelete(ctx, obj)

			By("Expecting an error to occur")
			Expect(err).To(HaveOccurred())
			Expect(warnings).To(BeNil())

			By("Validating the error message")
			Expect(err.Error()).To(ContainSubstring("deletion is denied for DatabaseClaim"))
			Expect(err.Error()).To(ContainSubstring(deletionOverrideLabel))
		})

		It("Should deny deletion if the deletion override label is set to false", func() {
			By("Setting the deletion override label to false")
			obj.SetLabels(map[string]string{deletionOverrideLabel: "false"})

			By("Calling ValidateDelete")
			warnings, err := validator.ValidateDelete(ctx, obj)

			By("Expecting an error to occur")
			Expect(err).To(HaveOccurred())
			Expect(warnings).To(BeNil())

			By("Validating the error message")
			Expect(err.Error()).To(ContainSubstring("deletion is denied for DatabaseClaim"))
			Expect(err.Error()).To(ContainSubstring(deletionOverrideLabel))
		})
	})

})
