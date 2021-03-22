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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DatabaseClaimSpec defines the desired state of DatabaseClaim
type DatabaseClaimSpec struct {
	// +kubebuilder:validation:MinLength=1

	// Specifies an indentifier for the application using the database.
	AppID string `json:"app_id,omitempty"`

	// +kubebuilder:validation:MinLength=0

	// Specifies the type of database to provision. Only postgres is supported.
	// +optional
	DBType string `json:"db_type,omitempty"`
}

// DatabaseClaimStatus defines the observed state of DatabaseClaim
type DatabaseClaimStatus struct {
	State          string                       `json:"state,omitempty"`
	UserCreateTime *metav1.Time                 `json:"userCreateTime,omitempty"`
	ConnectionInfo *DatabaseClaimConnectionInfo `json:"connectionInfo"`
}

type DatabaseClaimConnectionInfo struct {
	Hostname string `json:"hostname,omitempty"`
	Port     string `json:"port,omitempty"`
	Name     string `json:"db_name,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// DatabaseClaim is the Schema for the databaseclaims API
type DatabaseClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseClaimSpec   `json:"spec,omitempty"`
	Status DatabaseClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseClaimList contains a list of DatabaseClaim
type DatabaseClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseClaim{}, &DatabaseClaimList{})
}
