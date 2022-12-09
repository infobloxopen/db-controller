package dbclient

import "github.com/aws/aws-sdk-go-v2/aws"

type Client interface {
	CreateDatabase(dbName string) (bool, error)
	CreateUser(username, role, userPassword string) (bool, error)
	CreateGroup(dbName, username string) (bool, error)
	CreateDefaultExtentions(dbName string) error
	RenameUser(oldUsername string, newUsername string) error
	UpdateUser(oldUsername, newUsername, rolename, password string) error
	UpdatePassword(username string, userPassword string) error

	DBCloser
}

// DBClient is retired interface, use Client
type DBClient interface {
	Client
	CreateDataBase(name string) (bool, error)
}

type DBCloser interface {
	Close() error
}

type CredentialsProviderFunc aws.CredentialsProviderFunc
