package e2e

import (
	"fmt"
	"strings"

	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	crossplanegcp "github.com/upbound/provider-gcp/apis/alloydb/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getResourceStatus tries to find the status from the object interface
func getResourceStatus(obj client.Object) (xpv1.ResourceStatus, error) {

	switch obj.(type) {
	case *crossplanegcp.Instance:
		return obj.(*crossplanegcp.Instance).Status.ResourceStatus, nil
	case *crossplaneaws.DBInstance:
		return obj.(*crossplaneaws.DBInstance).Status.ResourceStatus, nil
	}

	return xpv1.ResourceStatus{}, fmt.Errorf("unsupported resource type")

}

func isResourceSynced(resourceStatus xpv1.ResourceStatus) bool {
	synced := xpv1.TypeSynced
	conditionTrue := corev1.ConditionTrue

	for _, condition := range resourceStatus.Conditions {
		if condition.Type == synced && condition.Status == conditionTrue {
			return true
		}
	}
	return false
}

// isResourceReady parses the resource status and returns true
// if the resource is ready
func isResourceReady(resourceStatus xpv1.ResourceStatus) bool {
	ready := xpv1.TypeReady
	conditionTrue := corev1.ConditionTrue

	for _, condition := range resourceStatus.Conditions {
		if condition.Type == ready && condition.Status == conditionTrue {
			return true
		}
	}
	return false
}

func extractVersion(message string) string {
	versionStr := ""
	splitMessage := strings.Split(message, " ")
	for i, word := range splitMessage {
		if word == "version" {
			versionStr = splitMessage[i+1] // This should be of format "15.3"
			break
		}
	}
	return versionStr
}
