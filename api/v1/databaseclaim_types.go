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

type DatabaseType string

const (
	Postgres DatabaseType = "postgres"
	MySQL    DatabaseType = "mysql"
)

type ImportType string

const (
	PostgresImport ImportType = "postgres"
	MySQLImport    ImportType = "mysql"
)

type SourceType string

const (
	PostgresSource SourceType = "postgres"
	S3BucketSource SourceType = "s3Bucket"
)

// Source is a union object for specifying the initial state of a DB that should be used when provisioning a DatabaseClaim
type Source struct {
	// Type specifies the type of source
	// +required
	Type string `json:"type"`

	// PostgresDatabase defines the connection information to an existing postgres db
	// +optional
	PostgresDatabase *PostgresDatabase `json:"postgresDatabase,omitempty"`

	// S3Bucket defines the location of a DB backup in an S3 bucket
	// +optional
	S3Bucket *S3Bucket `json:"s3Bucket,omitempty"`
}

type S3Bucket struct {
	// ImportType specifies what type of database that the bucket contains
	// +required
	ImportType ImportType `json:"databaseType"`

	// +required
	Region string `json:"region"`

	// +required
	Bucket string `json:"bucket"`
	// +required
	Path string `json:"path"`

	// AWSCredentials specifies a secret to use for connecting to the s3 bucket via AWS client
	// +optional
	AWSCredentials *SecretRef `json:"secretRef,omitempty"`
}

type PostgresDatabase struct {
	// DSN is the connection string used to reach the postgres database
	DSN string `json:"dsn,omitempty"`

	// SecretRef specifies a secret to use for connecting to the postgresdb (should be master/root)
	SecretRef *SecretRef `json:"secretRef,omitempty"`
}

type SecretRef struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// DatabaseClaimSpec defines the desired state of DatabaseClaim
type DatabaseClaimSpec struct {
	// Source specifies an existing postgres database or backup to use when initially provisioning the database.  If it already exists, this field is ignored
	// +optional
	Source *Source `json:"source,omitempty"`

	// UseExistingSource instructs the controller to perform user management on the database currently defined in the Source field.
	// If source is empty, this is ignored
	// If this is set, .source.Type and .type must match to use an existing source (since they must be the same)
	// If this field was set and becomes unset, migration of data will commence
	// +optional
	UseExistingSource *bool `json:"useExistingSource,omitempty"`

	// Specifies an indentifier for the application using the database.
	AppID string `json:"appId"`

	// Specifies the type of database to provision. Only postgres is supported.
	// +required
	Type DatabaseType `json:"type"`

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

	// The name of the database within InstanceLabel.
	DatabaseName string `json:"databaseName,omitempty"`

	// DSN key name.
	DSNName string `json:"dsnName"`

	// The optional Shape values are arbitrary and help drive instance selection
	// +optional
	Shape string `json:"shape"`

	// The optional MinStorageGB value requests the minimum database host storage capacity in GBytes
	// +optional
	MinStorageGB int `json:"minStorageGB"`

	// Tags
	Tags []Tag `json:"tags,omitempty"`
}

// Tag
type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
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
