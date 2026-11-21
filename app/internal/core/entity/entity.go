package entity

import (
	"encoding/json"
)

type (

	// Entity represents an entity in the system
	Entity struct {
		// EntityID is a composite of the entity's IDs
		EntityID

		// EntityTimestamp contains the timestamps of this entity, when it was created, updated and/or deleted
		EntityTimestamp

		// Metadata is a key value pairing of additional information for an entity
		metadata map[string]any

		Marshaler
	}

	// EntityParams are the parameters/arguments to create a new entity
	EntityParams struct {
		EntityIDParams
		EntityTimestampParams
		Metadata map[string]any `json:"metadata"`
	}
)

// NewEntity creates a new base entity
func NewEntity(params EntityParams) (Entity, error) {
	entityTimestamp, err := NewEntityTimestamp(params.EntityTimestampParams)
	if err != nil {
		return Entity{}, err
	}

	return Entity{
		EntityID:        NewEntityID(params.EntityIDParams),
		EntityTimestamp: entityTimestamp,
		metadata:        params.Metadata,
	}, nil
}

// CompositeID returns a combination of the system generated ID, key ID & generated UUID
func (e Entity) CompositeID() (KeyID, ID) {
	return e.keyID, e.id
}

// Metadata updates metadata for an entity returning a new copy of metadata
func (e Entity) Metadata() map[string]any {
	return e.metadata
}

// WithMetadata updates metadata for an entity returning a new copy of metadata
func (e Entity) WithMetadata(metadata map[string]any) Entity {
	e.metadata = metadata
	return e
}

// MetadataToBytes converts metadata to bytes. Returns an error if there is a failure to marshal the values
func (e Entity) MetadataToBytes() ([]byte, error) {
	b, err := json.Marshal(e.metadata)
	if err != nil {
		return nil, err
	}

	return b, nil
}

// MetadataToBytes converts metadata to bytes. Returns an error if there is a failure to marshal the values
func BytesToMetadata(data []byte) (map[string]any, error) {
	metadata := make(map[string]any)
	err := json.Unmarshal(data, &metadata)
	if err != nil {
		return nil, err
	}

	return metadata, nil
}
