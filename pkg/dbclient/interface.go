package dbclient

import "github.com/aws/aws-sdk-go-v2/aws"

type Clienter interface {
	Creater
	Updater
	Exister
	DBCloser

	ManageReplicationRole(username string, enableReplicationRole bool) error
	ManageSuperUserRole(username string, enableSuperUser bool) error
	ManageCreateRole(username string, enableCreateRole bool) error
	ManageSystemFunctions(dbName string, functions map[string]string) error
}

type Creater interface {
	CreateDatabase(dbName string) (bool, error)
	//Creates a user in the the database
	CreateUser(username, role, userPassword string) (bool, error)
	//Creates a role in the specified DB and SCHEMA - with access to this specific SCHEMA only
	CreateRole(dbName, rolename, schema string) (bool, error)
	CreateAdminRole(dbName, rolename, schema string) (bool, error)
	CreateRegularRole(dbName, rolename, schema string) (bool, error)
	CreateReadOnlyRole(dbName, rolename, schema string) (bool, error)
	CreateDefaultExtensions(dbName string) error
	CreateSpecialExtensions(dbName string, role string) error
	CreateSchema(schemaName string) (bool, error)
}

type Updater interface {
	RenameUser(oldUsername string, newUsername string) error
	UpdateUser(oldUsername, newUsername, rolename, password string) error
	UpdatePassword(username string, userPassword string) error
	AssignRoleToUser(username, rolename string) error
}

type Exister interface {
	// Checks if a schema exists in the database
	SchemaExists(schemaName string) (bool, error)
	// Checks if a usedr exists in the database
	UserExists(userName string) (bool, error)
	// Checks if a role exists in the database
	RoleExists(roleName string) (bool, error)
}

// DBClient is retired interface, use Client
type DBClient interface {
	Clienter
	CreateDataBase(name string) (bool, error)
}

type DBCloser interface {
	Close() error
}

type CredentialsProviderFunc aws.CredentialsProviderFunc
