package database

import (
	"errors"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("record not found")

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
