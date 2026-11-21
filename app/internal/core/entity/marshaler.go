package entity

// Marshaler is an interface for marshaling an entity
type Marshaler interface {
	// Marshal marshals an entity to a byte slice
	Marshal() ([]byte, error)

	// ToMap converts an entity to a map
	ToMap() map[string]any
}
