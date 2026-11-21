package entity

import (
	"fmt"
	"time"

	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

// EntityTimestamp are the timestamps for when an entity was created, updated and/or deleted
type (
	EntityTimestamp struct {
		// createdAt is when an entity was created
		createdAt time.Time

		// updatedAt is when an entity was last updated
		updatedAt time.Time

		// deletedAt is when an entity was deleted/removed from the system
		deletedAt *time.Time
	}

	// EntityTimestampParams with fields to create a new entity timestamp
	EntityTimestampParams struct {
		// CreatedAt is when an entity was created
		CreatedAt time.Time `json:"createdAt"`

		// UpdatedAt is when an entity was last updated
		UpdatedAt time.Time `json:"updatedAt"`

		// DeletedAt is when an entity was deleted/removed from the system
		DeletedAt *time.Time `json:"deletedAt"`
	}
)

// NewEntityTimestamp creates an entity timestamp
func NewEntityTimestamp(params EntityTimestampParams) (EntityTimestamp, error) {
	return parseEntityTimestamps(params.CreatedAt, params.UpdatedAt, params.DeletedAt)
}

// CreatedAt returns the timestamp this entity was created
func (et *EntityTimestamp) CreatedAt() time.Time {
	return et.createdAt
}

// WithCreatedAt returns a new copy of EntityTimestamp with the created at timestamp updated
func (et EntityTimestamp) WithCreatedAt(cat time.Time) EntityTimestamp {
	et.createdAt = cat
	return et
}

// UpdatedAt returns the timestamp this entity was updated
func (et *EntityTimestamp) UpdatedAt() time.Time {
	return et.updatedAt
}

// WithUpdatedAt returns a new copy of EntityTimestamp with the created at timestamp updated
func (et EntityTimestamp) WithUpdatedAt(uat time.Time) EntityTimestamp {
	et.updatedAt = uat
	return et
}

// Deleted returns the timestamp this entity was deleted
func (et *EntityTimestamp) DeletedAt() *time.Time {
	return et.deletedAt
}

// WithDeletedAt returns a new copy of EntityTimestamp with the created at timestamp updated
func (et EntityTimestamp) WithDeletedAt(dat time.Time) EntityTimestamp {
	et.deletedAt = &dat
	return et
}

// parseEntityTimestamps creates an EntityTimestamp with validation handled, if validation fails, an error is returned
func parseEntityTimestamps(createdAt, updatedAt time.Time, deletedAt *time.Time) (EntityTimestamp, error) {
	// validate timestamps

	if createdAt.After(updatedAt) {
		// invalid, created at time should not be after updated at timestamp
		return EntityTimestamp{}, errdefs.InvalidParameter(fmt.Errorf("created time %s can not be after updated time %s", createdAt, updatedAt))
	}

	// when there is a deleted at timestamp
	if deletedAt != nil {
		// it can not be before created nor updated at timestamps
		if deletedAt.Before(createdAt) || deletedAt.Before(updatedAt) {
			return EntityTimestamp{}, errdefs.InvalidParameter(fmt.Errorf("deleted time %s can not be before created time %s or updated time %s", deletedAt, createdAt, updatedAt))
		}
	}

	return EntityTimestamp{
		createdAt: createdAt.UTC().Round(time.Microsecond),
		updatedAt: updatedAt.UTC().Round(time.Microsecond),
		deletedAt: deletedAt,
	}, nil
}
