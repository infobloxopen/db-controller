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
	AppID string `json:"appId"`
	// +kubebuilder:validation:MinLength=0

	// Specifies the type of database to provision. Only postgres is supported.
	Type string `json:"type"`

	// In most cases the AppID will match the database name. In some cases, however, we will need to provide an optional override.
	DBNameOverride string `json:"dbNameOverride,omitempty"`

	// The name of the secret to use for storing the ConnectionInfo.  Must follow a naming convention that ensures it is unique.
	SecretName string `json:"secretName,omitempty"`

	// The matching fragment key name of the database instance that will host the database.
	InstanceLabel string `json:"instanceLabel,omitempty"`

	// The username that the application will use for accessing the database.
	Username string `json:"userName"`

	// The optional host name where the database instance is located.
	// If the value is omitted, then the host value from the matching InstanceLabel will be used.
	// +optional
	Host string `json:"host,omitempty"`

	// The optional port to use for connecting to the host.
	// If the value is omitted, then the host value from the matching InstanceLabel will be used.
	// +optional
	Port string `json:"port,omitempty"`

	// The name of the database instance.
	DatabaseName string `json:"databaseName,omitempty"`

	// DSN key name.
	DSNName string `json:"dsnName"`

	// The optional Shape values are arbitrary and help drive instance selection
	// +optional
	Shape string `json:"shape"`

	// The optional MinStorageGB value requests the minimum database host storage capacity in GBytes
	// +optional
	MinStorageGB int `json:"minStorageGB"`
}

// DatabaseClaimStatus defines the observed state of DatabaseClaim
type DatabaseClaimStatus struct {
	// Any errors related to provisioning this claim.
	Error string `json:"error,omitempty"`

	// Time the database was created
	DbCreatedAt *metav1.Time `json:"dbCreateAt,omitempty"`

	// The name of the label that was successfully matched against the fragment key names in the db-controller configMap
	MatchedLabel string `json:"matchLabel,omitempty"`

	ConnectionInfo *DatabaseClaimConnectionInfo `json:"connectionInfo"`

	// Time the connection info was updated/created.
	ConnectionInfoUpdatedAt *metav1.Time `json:"connectionUpdatedAt,omitempty"`

	// Time the user/password was updated/created
	UserUpdatedAt *metav1.Time `json:"userUpdatedAt,omitempty"`
}

type DatabaseClaimConnectionInfo struct {
	Host         string `json:"hostName,omitempty"`
	Port         string `json:"port,omitempty"`
	DatabaseName string `json:"databaseName,omitempty"`
	Username     string `json:"userName,omitempty"`
	Password     string `json:"password,omitempty"`
	SSLMode      string `json:"sslMode,omitempty"`
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
