/*


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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type UserType struct {
	UserName string `json:"username"`
}

type SchemaUserType struct {
	Name  string     `json:"name,omitempty"`
	Users []UserType `json:"users,omitempty"`
}

type SchemaUserClaimSpec struct {

	// Class is used to run multiple instances of dbcontroller.
	// +optional
	// +kubebuilder:default:="default"
	Class *string `json:"class"`

	// Schemas holds the schemas to be created and the user names to be created and granted access to this schema.
	Schemas []SchemaUserType `json:"schemas"`
}

// SchemaUserClaimStatus defines the observed state of SchemaUserClaim
type SchemaUserClaimStatus struct {
	// Any errors related to provisioning this claim.
	Error   string       `json:"error,omitempty"`
	Schemas SchemaStatus `json:"schemas,omitempty"`
}

type SchemaStatus struct {
	Name        string         `json:"name,omitempty"`
	Status      string         `json:"status,omitempty"`
	UsersStatus UserStatusType `json:"usersstatus,omitempty"`
}

type UserStatusType struct {
	UserName   string `json:"username"`
	UserStatus string `json:"userstatus"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Schema",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.usersStatus.userStatus`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=usc;usclaim
// +kubebuilder:subresource:status

// SchemaUserClaim is the Schema for the SchemaUserClaims API
type SchemaUserClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SchemaUserClaimSpec   `json:"spec,omitempty"`
	Status SchemaUserClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SchemaUserClaimList contains a list of SchemaUserClaim
type SchemaUserClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SchemaUserClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SchemaUserClaim{}, &SchemaUserClaimList{})
}
