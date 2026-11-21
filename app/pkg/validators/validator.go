package validators

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"time"

	"github.com/go-ozzo/ozzo-validation/is"
	"github.com/go-playground/validator/v10"
	"github.com/sanctumlabs/curtz/app/pkg"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"

	validation "github.com/go-ozzo/ozzo-validation"
)

// IsValidUrl validates a url
func IsValidUrl(url string) error {
	err := validation.Validate(url, validation.Required, is.URL)
	if err != nil {
		return errdefs.ErrInvalidURL
	}
	return nil
}

// IsValidUserId checks if a given userId is valid
func IsValidUserId(userId string) error {
	if userId == "" {
		return errdefs.ErrInvalidUserId
	}
	return nil
}

// IsValidUrlId checks if a given urlId is valid
func IsValidUrlId(urlId string) error {
	if urlId == "" {
		return errdefs.ErrInvalidUrlId
	}
	return nil
}

// IsValidCustomAlias checks if a given custom alias is valid
func IsValidCustomAlias(customAlias string) error {
	if customAlias != "" && len(customAlias) > pkg.ShortCodeLength {
		return errdefs.ErrInvalidCustomAlias
	}
	return nil
}

// IsValidExpirationTime checks if the given timestamp is a valid expiration timestamp
func IsValidExpirationTime(expirationTime time.Time) error {
	if expirationTime.In(time.UTC).Before(time.Now().In(time.UTC)) {
		return errdefs.ErrPastExpiration
	}
	return nil
}

var (
	phoneRegex = regexp.MustCompile(`^[\+]?[(]?[0-9]{3}[)]?[-\s\.]?[0-9]{3}[-\s\.]?[0-9]{4,6}$`)
)

// GetValidationErrMsg checks to see if the provided err is a validation error and
// returns the first validation error message.
func GetValidationErrMsg(s any, err error) (errMsg string) {
	fieldErrors := validator.ValidationErrors{}

	if ok := errors.As(err, &fieldErrors); ok {
		fieldErr := fieldErrors[0]
		fieldName := getStructTag(s, fieldErr.Field(), "json")

		switch fieldErr.Tag() {
		case "required":
			errMsg = fmt.Sprintf("%s is a required field", fieldName)
		default:
			errMsg = fmt.Sprintf("Invalid input on %s", fieldName)
		}
	}

	return errMsg
}

func getStructTag(s any, fieldName string, tagKey string) string {
	t := reflect.TypeOf(s)
	field, found := t.FieldByName(fieldName)

	if t.Kind() != reflect.Struct {
		return fieldName
	}

	if !found {
		return fieldName
	}

	return field.Tag.Get(tagKey)
}

// IsValidationError checks to see if error is of type validator.ValidationErrors.
func IsValidationError(err error) bool {
	return errors.As(err, &validator.ValidationErrors{})
}

// ValidateCurrencyCode validates that a given currency code follows ISO 4217 code
func ValidateCurrencyCode(code string) error {
	err := validation.Validate(code, is.CurrencyCode)

	if err != nil {
		return errdefs.InvalidParameter(fmt.Errorf("provided currency code %s is not a valid ISO4217 currency code", code))
	}

	return nil
}

// ValidatePhoneNumber validates a phone number
func ValidatePhoneNumber(phone string) error {
	if !phoneRegex.MatchString(phone) {
		return errdefs.InvalidParameter(fmt.Errorf("provided phone number %s is not a valid phone number", phone))
	}

	return nil
}

// ValidateEmail validates an email address
func ValidateEmail(email string) error {
	if err := validation.Validate(email, validation.Required, is.Email); err != nil {
		return errdefs.InvalidParameter(fmt.Errorf("provided email %q is not a valid email address", email))
	}
	return nil
}
