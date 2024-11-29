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
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
)

// SetupDbRoleClaimWebhookWithManager registers the webhook for DbRoleClaim in the manager.
func SetupDbRoleClaimWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&persistancev1.DbRoleClaim{}).
		WithValidator(&DbRoleClaimCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-persistance-atlas-infoblox-com-v1-dbroleclaim,mutating=false,failurePolicy=fail,sideEffects=None,groups=persistance.atlas.infoblox.com,resources=dbroleclaims,verbs=delete,versions=v1,name=vdbroleclaim-v1.kb.io,admissionReviewVersions=v1

type DbRoleClaimCustomValidator struct{}

var _ webhook.CustomValidator = &DbRoleClaimCustomValidator{}

func (v *DbRoleClaimCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *DbRoleClaimCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *DbRoleClaimCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	log := logf.FromContext(ctx).WithName("dbroleclaim-webhook")

	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		log.Error(err, "Unable to retrieve AdmissionRequest from context")
	}
	log.Info("Deletion request details", "username", req.UserInfo.Username, "groups", req.UserInfo.Groups, "uid", req.UserInfo.UID)

	roleClaim, ok := obj.(*persistancev1.DbRoleClaim)
	if !ok {
		return nil, fmt.Errorf("expected a DatabaseClaim object but got %T", obj)
	}

	log.Info("Validation for DatabaseClaim upon deletion", "name", roleClaim.Name)

	if value, exists := roleClaim.GetLabels()[deletionOverrideLabel]; exists && value == "true" {
		log.Info("Deletion override label found; allowing deletion", "name", roleClaim.Name)
		return nil, nil
	}

	return nil, fmt.Errorf("deletion is denied for DatabaseClaim '%s'; set annotation or label '%s=true' to override",
		roleClaim.Name, deletionOverrideLabel)
}
