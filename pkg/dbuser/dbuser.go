package dbuser

import (
	"strings"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
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

func (dbu DBUser) IsUserChanged(dbClaim *persistancev1.DatabaseClaim) bool {
	prevUsername := dbu.TrimUserSuffix(dbClaim.Status.ConnectionInfo.Username)

	if dbu.rolename != prevUsername && dbClaim.Status.ConnectionInfo.Username != "" {
		return true
	}

	return false
}

func (dbu DBUser) TrimUserSuffix(in string) string {
	var out string
	if strings.HasSuffix(in, SuffixA) {
		out = strings.TrimSuffix(in, SuffixA)
	} else {
		out = strings.TrimSuffix(in, SuffixB)
	}

	return out
}

func (dbu DBUser) GetUserA() string {
	return dbu.userA
}

func (dbu DBUser) GetUserB() string {
	return dbu.userB
}

func (dbu DBUser) NextUser(curUser string) string {
	var nextUser string

	if dbu.userA == curUser {
		nextUser = dbu.userB
	} else {
		nextUser = dbu.userA
	}

	return nextUser
}
