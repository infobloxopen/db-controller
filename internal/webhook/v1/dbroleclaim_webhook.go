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

	roleClaim, ok := obj.(*persistancev1.DbRoleClaim)
	if !ok {
		return nil, fmt.Errorf("expected a DbRoleClaim object but got %T", obj)
	}

	log.Info("DbRoleClaim deletion request details", "username", req.UserInfo.Username, "groups", req.UserInfo.Groups, "uid", req.UserInfo.UID, "name", roleClaim.Name)
	if value, exists := roleClaim.GetLabels()[deletionOverrideLabel]; exists && value == "enabled" {
		return nil, nil
	}

	return nil, fmt.Errorf("deletion is denied for DbRoleClaim '%s'; set label '%s=enabled' to override",
		roleClaim.Name, deletionOverrideLabel)
}
