package dbuser

import (
	"strings"
)

const (
	SuffixA = "_a"
	SuffixB = "_b"
)

// DBUser is a struct that holds the usernames for a given role.
type DBUser struct {
	rolename string
	userA    string
	userB    string
}

// NewDBUser returns a new DBUser instance.
func NewDBUser(baseName string) DBUser {
	return DBUser{
		rolename: baseName,
		userA:    baseName + SuffixA,
		userB:    baseName + SuffixB,
	}
}

// IsUserChanged checks if the given currentUserName has changed compared to the
// rolename of the DBUser instance.
func (dbu DBUser) IsUserChanged(currentUserName string) bool {

	if currentUserName == "" {
		return false
	}

	return TrimUserSuffix(currentUserName) != dbu.rolename
}

// TrimUserSuffix removes the suffixes from the given string.
func TrimUserSuffix(in string) string {
	return strings.TrimSuffix(strings.TrimSuffix(in, SuffixA), SuffixB)
}

// GetUserA returns the username for UserA.
func (dbu DBUser) GetUserA() string {
	return dbu.userA
}

// GetUserB returns the username for UserB.
func (dbu DBUser) GetUserB() string {
	return dbu.userB
}

// NextUser returns UserB unless the current user is already UserB.
// Invalid users also return UserB as the next user. This is to prevent
// changes in current behavior of the func.
func (dbu DBUser) NextUser(curUser string) string {

	if strings.HasSuffix(curUser, SuffixA) {
		return dbu.GetUserB()
	}

	return dbu.GetUserA()
}
