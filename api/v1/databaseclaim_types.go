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
	"fmt"
	"net/url"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

var (
	DSNKey    = "dsn.txt"
	DSNURIKey = "uri_dsn.txt"
)

type DatabaseType string

const (
	Postgres       DatabaseType = "postgres"
	AuroraPostgres DatabaseType = "aurora-postgresql"
)

type SQLEngine string

// SQL database engines.
const (
	PostgresqlEngine SQLEngine = "postgres"
)

type SourceDataType string

type DeletionPolicy string

const (
	// Delete the database instance when the resource is deleted.
	DeleteDeletionPolicy DeletionPolicy = "Delete"
	// Retain the database instance when the resource is deleted.
	OrphanDeletionPolicy DeletionPolicy = "Orphan"
)

// SourceDataFrom is a union object for specifying the initial state of a DB that should be used when provisioning a DatabaseClaim
type SourceDataFrom struct {
	// Type specifies the type of source
	// +required
	Type SourceDataType `json:"type"`

	// Database defines the connection information to an existing db
	// +optional
	Database *Database `json:"database,omitempty"`
}

// Database defines the details of an existing database to use as a backup restoration source
type Database struct {
	// DSN is the connection string used to reach the postgres database
	// must have protocol specifier at beginning (example: mysql://  postgres:// )
	// Deprecated: Use SecretRef dsn.txt instead
	// +optional
	DSN string `json:"dsn"`

	// SecretRef specifies a secret to use for connecting to the postgresdb (should be master/root)
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

	// Class is used to run multiple instances of dbcontroller.
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
	// +optional
	AppID string `json:"appId"`

	// Specifies the type of database to provision. Only postgres is supported.
	// +optional
	// +kubebuilder:default:=postgres
	Type DatabaseType `json:"type"`

	// Specifies the type of deletion policy to use when the resource is deleted.
	// It makes a lot of sense to not set it for most cases - this will default it to Orphan based on defaultDeletionPolicy in controllerConfig
	// If you are setting it to Delete, you should be aware that the database will be deleted when the resource is deleted. Hope you know what you are doing.
	// +optional
	// +kubebuilder:validation:Enum=Delete;delete;Orphan;orphan
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`

	// If provided, marks auto storage scalling to true for postgres DBinstance. The value represents the maximum allowed storage to scale upto.
	// For auroraDB instance, this value is ignored.
	MaxStorageGB int64 `json:"maxStorageGB,omitempty"`

	// The name of the secret to use for storing the ConnectionInfo.  Must follow a naming convention that ensures it is unique.
	SecretName string `json:"secretName,omitempty"`

	// The username that the application will use for accessing the database.
	Username string `json:"userName"`

	// The name of the database.
	// +required
	DatabaseName string `json:"databaseName"`

	// The version of the database.
	// +optional
	DBVersion string `json:"dbVersion"`

	// DSN is used to name the secret key that contains old style DSN for postgres.
	// This field is deprecated, update your code to access key
	// "dsn.txt" or preferrably "uri_dsn.txt" as this field is
	// not customizable any longer.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=dsn.txt
	DSNName string `json:"dsnName"`

	// The optional Shape values are arbitrary and help drive instance selection
	// +optional
	Shape string `json:"shape"`

	// The optional MinStorageGB value requests the minimum database host storage capacity in GBytes
	// +optional
	MinStorageGB int `json:"minStorageGB"`

	// BackupPolicy specifies the duration at which db backups are taken
	// +optional
	// +kubebuilder:validation:Enum=Bronze;Silver;Gold;Platinum
	BackupPolicy string `json:"backupPolicy,omitempty"`

	// RestoreFrom indicates the snapshot id to restore the Database from
	// +optional
	RestoreFrom string `json:"restoreFrom,omitempty"`

	// EnableReplicationRole will grant rds replication role to Username
	// This value is ignored if EnableSuperUser is set to true
	// +optional
	// +kubebuilder:default:=false
	EnableReplicationRole *bool `json:"enableReplicationRole"`

	// EnableSuperUser will grant rds_superuser and createrole role to Username
	// This value is ignored if {{ .Values.controllerConfig.supportSuperUserElevation }} is set to false
	// +optional
	// +kubebuilder:default:=false
	EnableSuperUser *bool `json:"enableSuperUser"`

	// Tags
	// +optional
	// +nullable
	Tags []Tag `json:"tags,omitempty"`

	// The weekly time range during which system maintenance can occur.
	//
	// Valid for Cluster Type: Aurora DB clusters and Multi-AZ DB clusters
	//
	// The default is a 30-minute window selected at random from an 8-hour block
	// of time for each Amazon Web Services Region, occurring on a random day of
	// the week. To see the time blocks available, see Adjusting the Preferred DB
	// Cluster Maintenance Window (https://docs.aws.amazon.com/AmazonRDS/latest/AuroraUserGuide/USER_UpgradeDBInstance.Maintenance.html#AdjustingTheMaintenanceWindow.Aurora)
	// in the Amazon Aurora User Guide.
	//
	// Constraints:
	//
	//    * Must be in the format ddd:hh24:mi-ddd:hh24:mi.
	//
	//    * Days must be one of Mon | Tue | Wed | Thu | Fri | Sat | Sun.
	//
	//    * Must be in Universal Coordinated Time (UTC).
	//
	//    * Must be at least 30 minutes.
	PreferredMaintenanceWindow *string `json:"preferredMaintenanceWindow,omitempty"`
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
	NewDB Status `json:"newDB,omitempty"`
	//track the status of the active db being used by the application
	ActiveDB Status `json:"activeDB,omitempty"`
	//tracks status of DB migration. if empty, not started.
	//non empty denotes migration in progress, unless it is S_Completed
	MigrationState string `json:"migrationState,omitempty"`
	// tracks the DB which is migrated and not more operational
	OldDB StatusForOldDB `json:"oldDB,omitempty"`
}

type StatusForOldDB struct {
	// Time the connection info was updated/created.
	ConnectionInfo *DatabaseClaimConnectionInfo `json:"connectionInfo,omitempty"`

	// Version of the provisioned Database
	DBVersion string `json:"dbversion,omitempty"`

	// The optional Shape values are arbitrary and help drive instance selection
	Shape string `json:"shape,omitempty"`

	// Specifies the type of database to provision. Only postgres is supported.
	Type DatabaseType `json:"type,omitempty"`

	// Time at the process of post migration actions initiated
	PostMigrationActionStartedAt *metav1.Time `json:"postMigrationActionStartedAt,omitempty"`

	// DbState of the DB. inprogress, "", ready
	DbState DbState `json:"DbState,omitempty"`

	// The optional MinStorageGB value requests the minimum database host storage capacity in GBytes
	MinStorageGB int `json:"minStorageGB,omitempty"`
}

type Status struct {
	// Time the database was created
	DbCreatedAt *metav1.Time `json:"dbCreateAt,omitempty"`

	// The optional Connection information to the database.
	ConnectionInfo *DatabaseClaimConnectionInfo `json:"connectionInfo,omitempty"`

	// Version of the provisioned Database
	DBVersion string `json:"dbversion,omitempty"`

	// The optional Shape values are arbitrary and help drive instance selection
	Shape string `json:"shape,omitempty"`

	// The optional MinStorageGB value requests the minimum database host storage capacity in GBytes
	MinStorageGB int `json:"minStorageGB,omitempty"`

	// If provided, marks auto storage scalling to true for postgres DBinstance. The value represents the maximum allowed storage to scale upto.
	// For auroraDB instance, this value is ignored.
	MaxStorageGB int64 `json:"maxStorageGB,omitempty"`

	// Specifies the type of database to provision. Only postgres is supported.
	Type DatabaseType `json:"type,omitempty"`

	// Time the connection info was updated/created.
	ConnectionInfoUpdatedAt *metav1.Time `json:"connectionUpdatedAt,omitempty"`

	// Time the user/password was updated/created
	UserUpdatedAt *metav1.Time `json:"userUpdatedAt,omitempty"`

	// DbState of the DB. inprogress, "", ready
	DbState DbState `json:"DbState,omitempty"`

	// SourceDataFrom specifies an existing database or backup to use when initially provisioning the database.
	// if the dbclaim has already provisioned a database, this field is ignored
	// This field used when claim is use-existing-db and attempting to migrate to newdb
	// +optional
	SourceDataFrom *SourceDataFrom `json:"sourceDataFrom,omitempty"`
}

// DbState keeps track of state of the DB.
type DbState string

const (
	Ready                   DbState = "ready"
	InProgress              DbState = "in-progress"
	UsingExistingDB         DbState = "using-existing-db"
	UsingSharedHost         DbState = "using-shared-host"
	PostMigrationInProgress DbState = "post-migration-in-progress"
)

type DatabaseClaimConnectionInfo struct {
	Host         string `json:"hostName,omitempty"`
	Port         string `json:"port,omitempty"`
	DatabaseName string `json:"databaseName,omitempty"`
	Username     string `json:"userName,omitempty"`
	Password     string `json:"password,omitempty"`
	SSLMode      string `json:"sslMode,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="DB",type=string,JSONPath=`.spec.databaseName`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.activeDB.DbState`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="MigrationState",type="string",priority=1,JSONPath=".status.migrationState"
// +kubebuilder:resource:shortName=dbc
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

func (c *DatabaseClaimConnectionInfo) Uri() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%s://%s:%s@%s:%s/%s?sslmode=%s", "postgres",
		url.QueryEscape(c.Username), url.QueryEscape(c.Password), c.Host, c.Port, url.QueryEscape(c.DatabaseName), c.SSLMode)
}

// ParseUri parses a correctly formatted database URI into a
// DatabaseClaimConnectionInfo
func ParseUri(dsn string) (*DatabaseClaimConnectionInfo, error) {

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
