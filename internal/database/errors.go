package database

import (
	"errors"

	"gorm.io/gorm"
)

var (
	ErrNotFound      = errors.New("record not found")
	ErrInvalidLookup = errors.New("invalid lookup key")
	ErrDuplicateKey  = errors.New("duplicate key")
)

func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) {
		return true
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}
	return false
}

// WrapDuplicateKey returns ErrDuplicateKey if err is a GORM duplicate-key error,
// otherwise returns err unchanged. Use this at the repository boundary so that
// callers outside the database package never depend on gorm error types.
func WrapDuplicateKey(err error) error {
	if err != nil && errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrDuplicateKey
	}
	return err
}
