package entities

import (
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/utils"
)

// Email is a domain entity for email
type Email struct {
	value string
}

// NewEmail creates a new email
func NewEmail(value string) (*Email, error) {
	if utils.IsEmailValid(value) {
		return &Email{
			value: value,
		}, nil
	}
	return nil, errdefs.ErrEmailInvalid
}

// SetValue sets the value of an email address
func (e *Email) SetValue(value string) error {
	if !utils.IsEmailValid(value) {
		return errdefs.ErrEmailInvalid
	}
	e.value = value
	return nil
}

// GetValue returns the value of an email address
func (e *Email) GetValue() string {
	return e.value
}
