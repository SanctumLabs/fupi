package identity

import (
	"fmt"
)

// UserFullName is a value object representing a user's full in the system.
type UserFullName struct {
	firstName string
	lastName  string
}

// NewUserFullName is a factory method that creates a user's full name
func NewUserFullName(firstName, lastName string) (UserFullName, error) {
	if len(firstName) == 0 && len(lastName) == 0 {
		return UserFullName{}, fmt.Errorf("first name %s and last name %s cannot both be empty", firstName, lastName)
	}

	if len(firstName) == 0 {
		return UserFullName{}, fmt.Errorf("first name %s is invalid", firstName)
	}

	return UserFullName{
		firstName: firstName,
	}, nil
}

// FirstName is the user's first name
func (ufn *UserFullName) FirstName() string {
	return ufn.firstName
}

// LastName is the user's last name
func (ufn *UserFullName) LastName() string {
	return ufn.lastName
}

// Value is the user's full name
func (ufn *UserFullName) Value() string {
	return fmt.Sprintf("%s %s", ufn.firstName, ufn.lastName)
}

// SetFirstName sets the user's first name
func (ufn *UserFullName) SetFirstName(firstName string) error {
	if len(firstName) == 0 {
		return fmt.Errorf("first name %s is invalid", firstName)
	}
	ufn.firstName = firstName
	return nil
}

// SetLastName sets the user's last name
func (ufn *UserFullName) SetLastName(lastName string) error {
	if len(lastName) == 0 {
		return fmt.Errorf("last name %s is invalid", lastName)
	}
	ufn.lastName = lastName
	return nil
}

func (ufn *UserFullName) String() string {
	return fmt.Sprintf("%s %s", ufn.firstName, ufn.lastName)
}
