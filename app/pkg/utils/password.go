package utils

import (
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"golang.org/x/crypto/bcrypt"
)

// HashPassword hashes the user password
func HashPassword(value string) (string, error) {
	if value == "" {
		return "", errdefs.ErrInvalidPasswordLen
	}

	pwd := []byte(value)

	hash, err := bcrypt.GenerateFromPassword(pwd, bcrypt.DefaultCost)

	if err != nil {
		return "", err
	}

	return string(hash), nil
}

// CompareHashAndPassword compares the password hash against the passed in password string
func CompareHashAndPassword(hash, password string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		return false, err
	}
	return true, nil
}
