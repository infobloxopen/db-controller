package dbuser

import (
	"strings"
)

const (
	SuffixA = "_a"
	SuffixB = "_b"
)

type DBUser struct {
	rolename string
	userA    string
	userB    string
}

func NewDBUser(baseName string) DBUser {
	return DBUser{
		rolename: baseName,
		userA:    baseName + SuffixA,
		userB:    baseName + SuffixB,
	}
}

func (dbu DBUser) IsUserChanged(currentUserName string) bool {

	if currentUserName == "" {
		return false
	}

	return TrimUserSuffix(currentUserName) != dbu.rolename
}

func TrimUserSuffix(in string) string {
	return strings.TrimSuffix(strings.TrimSuffix(in, SuffixA), SuffixB)
}

func (dbu DBUser) GetUserA() string {
	return dbu.userA
}

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
