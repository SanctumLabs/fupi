package utils

import (
	"encoding/json"
	"fmt"
	"maps"
	"math/rand"
	"regexp"
	"time"

	"github.com/samber/lo"
)

// IsEmailValid checks if an email address is valid
func IsEmailValid(email string) bool {
	pattern := `^(([^<>()\[\]\\.,;:\s@"]+(\.[^<>()\[\]\\.,;:\s@"]+)*)|(".+"))@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}])|(([a-zA-Z\-0-9]+\.)+[a-zA-Z]{2,}))$`
	re := regexp.MustCompile(pattern)
	return re.Match([]byte(email))
}

// Map maps a collection/slice of objects of type T to type R with the given function fn
func Map[T any, R any](collection []T, fn func(item T, idx int) R) []R {
	return lo.Map(collection, fn)
}

// MapWithError maps a collection/slice of objects of type T to type R with the given function fn and potentially returns an error
func MapWithError[T any, R any](collection []T, fn func(item T, idx int) (R, error)) ([]R, error) {
	result := make([]R, len(collection))

	for i, item := range collection {
		r, err := fn(item, i)
		if err != nil {
			return nil, err
		}
		result[i] = r
	}

	return result, nil
}

// Filter filters data based on a certain predicate
func Filter[T any](data []T, predicate func(T) bool) []T {
	filtered := make([]T, 0)

	for _, d := range data {
		if predicate(d) {
			filtered = append(filtered, d)
		}
	}

	return filtered
}

// MapToBytes converts a map of string to any to bytes.
// Returns an error if there is a failure to marshal the values
func MapToBytes(m map[string]any) ([]byte, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal map to bytes: %w", err)
	}

	return b, nil
}

// BytesToMap converts bytes to a map of string to any.
// Returns an error if there is a failure to unmarshal the values
func BytesToMap(data []byte) (map[string]any, error) {
	metadata := make(map[string]any)
	err := json.Unmarshal(data, &metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata to bytes: %w", err)
	}

	return metadata, nil
}

// GetZeroValue returns the zero value for a generic type
func GetZeroValue[T any]() T {
	var result T
	return result
}

// IsZero reports whether v is the zero value of its type (for comparables).
func IsZero[T comparable](v T) bool {
	return v == *new(T)
}

// Deref returns the value pointed to by p, or the zero value of T if p is nil.
func Deref[T any](p *T) T {
	if p == nil {
		return GetZeroValue[T]()
	}
	return *p
}

// IsEmpty checks if a value is empty. This is useful for checking if a value is zero or nil
func IsEmpty[T comparable](s T) bool {
	return IsZero(s)
}

// EitherOr returns first unless it is the zero value of its type, in which case it returns second.
func EitherOr[T comparable](first, second T) T {
	if IsZero(first) {
		return second
	}
	return first
}

// IsAllDigits checks if a string contains only digits
func IsAllDigits(s string) bool {
	for _, char := range s {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// Min returns the minimum of two integers
// This is a utility function to avoid importing math package for simple min operation
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MergeMaps merges two maps into one
func MergeMaps(m1, m2 map[string]any) map[string]any {
	result := make(map[string]any)

	// Copy all key-value pairs from m1
	maps.Copy(result, m1)

	// Copy all key-value pairs from m2 (overwrites m1 values if keys exist)
	maps.Copy(result, m2)

	return result
}

// DeepMergeMaps merges two maps into one
func DeepMergeMaps(m1, m2 map[string]any) map[string]any {
	result := make(map[string]any)

	// Copy all key-value pairs from m1
	maps.Copy(result, m1)

	// Merge values from m2
	for k, v := range m2 {
		if existingVal, exists := result[k]; exists {
			// If both values are maps, merge them recursively
			if existingMap, ok := existingVal.(map[string]any); ok {
				if newMap, ok := v.(map[string]any); ok {
					result[k] = DeepMergeMaps(existingMap, newMap)
					continue
				}
			}
		}
		// Otherwise, overwrite with the new value
		result[k] = v
	}

	return result
}

// PickRandomElement returns a random element from the provided slice.
// It uses Go generics (the 'T any' type parameter) to work with slices of any type.
//
// The function returns two values:
//   - The randomly selected element of type T
//   - An error if the slice is empty (you can't pick from nothing!)
//
// Example usage:
//
//	names := []string{"Alice", "Bob", "Charlie"}
//	randomName, err := PickRandomElement(names)
//
//	numbers := []int{1, 2, 3, 4, 5}
//	randomNum, err := PickRandomElement(numbers)
func PickRandomElement[T any](slice []T) (T, error) {
	// First, we need to handle the edge case of an empty slice
	// We can't pick a random element if there are no elements!
	if len(slice) == 0 {
		var zero T // Create a zero value of type T to return
		return zero, fmt.Errorf("cannot pick random element from empty slice")
	}

	// Generate a random index within the valid range of the slice
	// rand.Intn(n) returns a random number from 0 to n-1, which is perfect
	// since slice indices go from 0 to len(slice)-1
	randomIndex := rand.Intn(len(slice))

	// Return the element at that random index with no error
	return slice[randomIndex], nil
}

// GenerateRandomInt generates a random integer in the half open interval where the upper limit is defined.
// if the upper limit is set to a value less than or equal to 0, this will be reset to a deterministic random value
// in the range 1 < 100 were 100 is excluded from the range
// to avoid panicking.
func GenerateRandomInt(limits ...int) int {
	upperLimit := 100
	if len(limits) > 0 {
		limit := limits[0]
		if limit <= 0 {
			upperLimit = rand.Intn(100)
		}
	}

	source := rand.NewSource(time.Now().UnixNano())
	random := rand.New(source)

	return random.Intn(upperLimit)
}
