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
	"fmt"
	"net/url"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type DatabaseType string

const (
	Postgres DatabaseType = "postgres"
	MySQL    DatabaseType = "mysql"
	Aurora   DatabaseType = "aurora"
)

type SQLEngine string

// SQL database engines.
const (
	MysqlEngine      SQLEngine = "mysql"
	PostgresqlEngine SQLEngine = "postgres"
)

type SourceDataType string

const (
	DatabaseSource SourceDataType = "database"
	S3Source       SourceDataType = "s3"
)

// SourceDataFrom is a union object for specifying the initial state of a DB that should be used when provisioning a DatabaseClaim
type SourceDataFrom struct {
	// Type specifies the type of source
	// +required
	Type SourceDataType `json:"type"`

	// Database defines the connection information to an existing db
	// +optional
	Database *Database `json:"database,omitempty"`

	// S3 defines the location of a DB backup in an S3 bucket
	// +optional
	S3 *S3BackupConfiguration `json:"s3,omitempty"`
}

// S3BackupConfiguration defines the details of the S3 backup to restore from.
type S3BackupConfiguration struct {
	// +required
	Region string `json:"region"`

	// +required
	Bucket string `json:"bucket"`

	// Prefix is the path prefix of the S3 bucket within which the backup to restore is located.
	// +optional
	Prefix *string `json:"prefix,omitempty"`

	// SourceEngine is the engine used to create the backup.
	SourceEngine *SQLEngine `json:"sourceEngine"`

	// SourceEngineVersion is the version of the engine used to create the backup.
	// Example: "5.7.30"
	SourceEngineVersion *string `json:"sourceEngineVersion"`

	// SecretRef specifies a secret to use for connecting to the s3 bucket via AWS client
	// TODO: document/validate the secret format required
	// +optional
	SecretRef *SecretRef `json:"secretRef,omitempty"`
}

// Database defines the details of an existing database to use as a backup restoration source
type Database struct {
	// DSN is the connection string used to reach the postgres database
	// must have protocol specifier at beginning (example: mysql://  postgres:// )
	DSN string `json:"dsn"`

	// SecretRef specifies a secret to use for connecting to the postgresdb (should be master/root)
	// TODO: document/validate the secret format required
	// +optional
	SecretRef *SecretRef `json:"secretRef,omitempty"`
}

// SecretRef provides the information necessary to reference a Kubernetes Secret
type SecretRef struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// DatabaseClaimSpec defines the desired state of DatabaseClaim
type DatabaseClaimSpec struct {
	// class is used to run multiple instances of dbcontroller.
	// +optional
	// +kubebuilder:default:="default"

	Class *string `json:"class"`

	// SourceDataFrom specifies an existing database or backup to use when initially provisioning the database.
	// if the dbclaim has already provisioned a database, this field is ignored
	// +optional
	SourceDataFrom *SourceDataFrom `json:"sourceDataFrom,omitempty"`

	// UseExistingSource instructs the controller to perform user management on the database currently defined in the SourceDataFrom field.
	// If sourceDataFrom is empty, this is ignored
	// If this is set, .sourceDataFrom.Type and .type must match to use an existing source (since they must be the same)
	// If this field was set and becomes unset, migration of data will commence
	// +optional
	// +kubebuilder:default:=false
	UseExistingSource *bool `json:"useExistingSource"`

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
	// +optional
	// +nullable
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
	//track the status of new db in the process of being created
	NewDB *Status `json:"newDB,omitempty"`
	//track the status of the active db being used by the application
	ActiveDB *Status `json:"activeDB,omitempty"`
	//tracks status of DB migration. if empty, not started.
	//non empty denotes migration in progress, unless it is S_Completed
	MigrationState string `json:"migrationState,omitempty"`
}

type Status struct {
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

func (c *DatabaseClaimConnectionInfo) Dsn() string {
	return fmt.Sprintf("%s://%s:%s@%s:%s/%s?sslmode=%s", "postgres",
		c.Username, url.QueryEscape(c.Password), c.Host, c.Port, c.DatabaseName, c.SSLMode)
}

func ParseDsn(dsn string) (*DatabaseClaimConnectionInfo, error) {

	c := DatabaseClaimConnectionInfo{}

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}

	c.Host = u.Hostname()
	c.Port = u.Port()
	c.Username = u.User.Username()
	c.Password, _ = u.User.Password()
	db := strings.Split(u.Path, "/")
	if len(db) <= 1 {
		return &c, err
	}
	c.DatabaseName = db[1]

	m, _ := url.ParseQuery(u.RawQuery)
	c.SSLMode = m.Get("sslmode")

	return &c, nil
}
