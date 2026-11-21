//go:build legacy

package entities

import (
	"time"

	"github.com/sanctumlabs/curtz/app/pkg/identifier"
)

// BaseEntity is a base model for all models
type (
	BaseEntity struct {
		// ID is the unique identifier for the entity
		id identifier.ID

		// Metadata is a map of any additional metadata for the entity
		metadata map[string]any

		// CreatedAt is the date when the entity was created
		createdAt time.Time

		// UpdatedAt is the date when the entity was updated
		updatedAt time.Time

		deletedAt *time.Time
	}

	// BaseEntity is a base model for all models
	BaseEntityParams struct {
		// ID is the unique identifier for the entity
		ID identifier.ID

		// Metadata is a map of any additional metadata for the entity
		Metadata map[string]any

		// CreatedAt is the date when the entity was created
		CreatedAt time.Time

		// UpdatedAt is the date when the entity was updated
		UpdatedAt time.Time

		// DeletedAt is the date when the entity was deleted (optional)
		DeletedAt *time.Time
	}
)

// NewBaseEntity creates a new base entity
func NewBaseEntity(params BaseEntityParams) BaseEntity {
	return BaseEntity{
		id:        params.ID,
		metadata:  params.Metadata,
		createdAt: params.CreatedAt,
		updatedAt: params.UpdatedAt,
		deletedAt: params.DeletedAt,
	}
}

func (be *BaseEntity) ID() identifier.ID {
	return be.id
}

// GetMetadata returns the metadata value for the given key, along with a boolean indicating if the key exists.
func (be *BaseEntity) GetMetadata(key string) (any, bool) {
	if be.metadata == nil {
		return nil, false
	}
	value, exists := be.metadata[key]
	return value, exists
}

// Metadata returns the entire metadata map for the entity.
func (be *BaseEntity) Metadata() map[string]any {
	return be.metadata
}

// SetMetadata sets the entire metadata map for the entity, replacing any existing metadata.
func (be *BaseEntity) SetMetadata(metadata map[string]any) {
	be.metadata = metadata
}

// UpdateMetadata updates or adds a single key-value pair in the metadata map for the entity.
func (be *BaseEntity) UpdateMetadata(key string, value any) {
	if be.metadata == nil {
		be.metadata = make(map[string]any)
	}
	be.metadata[key] = value
}

func (be *BaseEntity) UpdateTimestamp() {
	be.updatedAt = time.Now()
}

// UpdatedAt returns the last updated timestamp of the entity.
func (be *BaseEntity) UpdatedAt() time.Time {
	return be.updatedAt
}

// CreatedAt returns the creation timestamp of the entity.
func (be *BaseEntity) CreatedAt() time.Time {
	return be.createdAt
}

// DeletedAt returns the deletion timestamp of the entity, or nil if it has not been deleted.
func (be *BaseEntity) DeletedAt() *time.Time {
	return be.deletedAt
}

// IsDeleted returns true if the entity has been marked as deleted.
func (be *BaseEntity) IsDeleted() bool {
	return be.deletedAt != nil
}

// MarkAsDeleted sets the deletion timestamp to the current time, marking the entity as deleted.
func (be *BaseEntity) MarkAsDeleted() {
	now := time.Now()
	be.deletedAt = &now
}

// Restore removes the deletion timestamp, effectively restoring the entity if it was previously marked as deleted.
func (be *BaseEntity) Restore() {
	be.deletedAt = nil
}
