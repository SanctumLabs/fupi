package entity

import (
	"fmt"
)

type (
	// EntityID is a composite of IDs used for an entity
	EntityID struct {
		// ID is a unique ID of an entity
		id ID

		// KeyID is a unique Key ID for an entity used in sorting and storing in cache entries or lookup
		keyID KeyID
	}

	EntityIDParams struct {
		// ID is a unique ID of an entity
		ID ID `json:"id"`

		// KeyID is a unique Key ID for an entity used in sorting
		KeyID KeyID `json:"keyId"`
	}
)

// NewEntityID creates a new unique entity ID from the provided params
func NewEntityID(params EntityIDParams) EntityID {
	eid := params.ID
	keyId := params.KeyID

	return EntityID{
		id:    eid,
		keyID: keyId,
	}
}

// ID returns the ID
func (eid EntityID) ID() ID {
	return eid.id
}

// NodeId returns the node ID of the ID, which is the last 12 hex digits
func (eid EntityID) NodeId() string {
	return string(eid.id.NodeID())
}

// Urn returns the urn of the ID
func (eid EntityID) Urn() string {
	return eid.id.URN()
}

// TimeLow returns the time low of the ID
func (eid EntityID) TimeLow() string {
	timeLow := eid.id[0:4]
	return string(timeLow)
}

// WithID sets a new ID ard returns a copy of EntityID
func (eid EntityID) WithID(newId ID) EntityID {
	eid.id = newId
	return eid
}

// KeyID returns the Key ID
func (eid EntityID) KeyID() KeyID {
	return eid.keyID
}

// WithKeyID sets a new Key ID and returns a copy of the entity ID
func (eid EntityID) WithKeyID(keyId KeyID) EntityID {
	eid.keyID = keyId
	return eid
}

// String returns a string representation of the ID
func (eid EntityID) String() string {
	return fmt.Sprintf("%s-%s", eid.keyID, eid.id)
}
