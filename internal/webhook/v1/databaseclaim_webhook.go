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

const deletionOverrideLabel = "persistance.atlas.infoblox.com/allow-deletion"

// SetupDatabaseClaimWebhookWithManager registers the webhook for DatabaseClaim in the manager.
func SetupDatabaseClaimWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&persistancev1.DatabaseClaim{}).
		WithValidator(&DatabaseClaimCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-persistance-atlas-infoblox-com-v1-databaseclaim,mutating=false,failurePolicy=fail,sideEffects=None,groups=persistance.atlas.infoblox.com,resources=databaseclaims,verbs=delete,versions=v1,name=vdatabaseclaim-v1.kb.io,admissionReviewVersions=v1

type DatabaseClaimCustomValidator struct{}

var _ webhook.CustomValidator = &DatabaseClaimCustomValidator{}

func (v *DatabaseClaimCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *DatabaseClaimCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type DatabaseClaim.
func (v *DatabaseClaimCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	log := logf.FromContext(ctx).WithName("databaseclaim-webhook")

	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		log.Error(err, "Unable to retrieve AdmissionRequest from context")
	}

	claim, ok := obj.(*persistancev1.DatabaseClaim)
	if !ok {
		return nil, fmt.Errorf("expected a DatabaseClaim object but got %T", obj)
	}

	log.Info("DatabaseClaim deletion request details", "username", req.UserInfo.Username, "groups", req.UserInfo.Groups, "uid", req.UserInfo.UID, "name", claim.Name)
	if value, exists := claim.GetLabels()[deletionOverrideLabel]; exists && value == "enabled" {
		return nil, nil
	}

	return nil, fmt.Errorf("deletion is denied for DatabaseClaim '%s'; set label '%s=enabled' to override",
		claim.Name, deletionOverrideLabel)
}
