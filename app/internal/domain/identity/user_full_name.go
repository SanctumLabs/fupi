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
		lastName:  lastName,
	}, nil
}

// FirstName is the user's first name
func (ufn *UserFullName) FirstName() string {
	return ufn.firstName
}

func (ufn *UserFullName) WithFirstName(firstName string) (UserFullName, error) {
	if len(firstName) == 0 {
		return UserFullName{}, fmt.Errorf("first name %s is invalid", firstName)
	}
	return UserFullName{
		firstName: firstName,
		lastName:  ufn.lastName,
	}, nil
}

// LastName is the user's last name
func (ufn *UserFullName) LastName() string {
	return ufn.lastName
}

func (ufn *UserFullName) WithLastName(lastName string) (UserFullName, error) {
	if len(lastName) == 0 {
		return UserFullName{}, fmt.Errorf("last name %s is invalid", lastName)
	}
	return UserFullName{
		firstName: ufn.firstName,
		lastName:  lastName,
	}, nil
}

// Value is the user's full name. The last name is optional, so a user with only a first
// name renders as just that, with no trailing space.
func (ufn *UserFullName) Value() string {
	if ufn.lastName == "" {
		return ufn.firstName
	}
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
	return fmt.Sprintf("FullName(firstName=%s lastName=%s)", ufn.firstName, ufn.lastName)
}
