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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DbRoleClaimSpec defines the desired state of DbRoleClaim
type DbRoleClaimSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Class is used to run multiple instances of dbcontroller.
	// +optional
	// +kubebuilder:default:="default"
	Class *string `json:"class"`

	SourceDatabaseClaim *SourceDatabaseClaim `json:"sourceDatabaseClaim"`

	// The name of the secret to use for storing the ConnectionInfo.  Must follow a naming convention that ensures it is unique.
	SecretName string `json:"secretName,omitempty"`
}

// SourceDatabaseClaim defines the DatabaseClaim which owns the actual database
type SourceDatabaseClaim struct {
	// Namespace of the source databaseclaim
	// +kubebuilder:default:="default"
	Namespace string `json:"namespace"`

	// Name  of the source databaseclaim
	Name string `json:"name"`
}

// DbRoleClaimStatus defines the observed state of DbRoleClaim
type DbRoleClaimStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Any errors related to provisioning this claim.
	Error string `json:"error,omitempty"`

	// Identifies the databaseclaim this CR is associated with
	MatchedSourceClaim string `json:"matchedSourceClaim,omitempty"`

	// Identifies the source secret this claim in inheriting from
	SourceSecret string `json:"sourceSecret,omitempty"`

	// Tracks the resourceVersion of the source secret. Used to identify changes to the secret and to trigger a sync
	SourceSecretResourceVersion string `json:"sourceSecretResourceVersion,omitempty"`

	// Time the secret attached to this claim was created
	SecretCreatedAt *metav1.Time `json:"secretCreatedAt,omitempty"`

	// Time the secret attached to this claim was updated
	SecretUpdatedAt *metav1.Time `json:"secretUpdatedAt,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// DbRoleClaim is the Schema for the dbroleclaims API
type DbRoleClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DbRoleClaimSpec   `json:"spec,omitempty"`
	Status DbRoleClaimStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DbRoleClaimList contains a list of DbRoleClaim
type DbRoleClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DbRoleClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DbRoleClaim{}, &DbRoleClaimList{})
}
