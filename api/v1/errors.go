package v1

import "errors"

var (
	ErrInvalidDBType    = errors.New("invalid database type")
	ErrInvalidDBVersion = errors.New("invalid database version")
)
