// Package repository defines the interfaces for repositories that will be implemented in the infrastructure layer
package repository

import (
	"context"

	"github.com/sanctumlabs/curtz/app/internal/pkg/common"
)

type (
	FetchRecordsResponse[T any] struct {
		// Records is a slice of records of type T
		Records []T
		// Total is the total number of records available in the underlying storage
		Total int
		// Page is the current page number
		Page int
		// Size is the number of records on this page
		Size int
	}

	// ReadRepositoryPort defines a method set used to handle reading of data for a given entity from an underlying storage
	// implementation
	ReadRepositoryPort[T any] interface {
		// GetById retrieves an entity by its ID
		FetchById(ctx context.Context, id string) (T, error)

		// GetAll retrieves all entities given a set of request parameters
		FetchAll(ctx context.Context, params common.RequestParams) (FetchRecordsResponse[T], error)
	}

	// WriteRepositoryPort defines a method set used to handle writing of data for a given entity to an underlying storage
	// implementation
	WriteRepositoryPort[T any] interface {
		// Create creates a given entity
		Create(ctx context.Context, entity T) (T, error)

		// Update updates a given entity
		Update(ctx context.Context, entity T) (T, error)

		// SoftDelete marks a record as deleted but does not permanently delete it from the system
		SoftDelete(ctx context.Context, id string) error

		// Delete deletes a given entity by its ID
		Delete(ctx context.Context, id string) error
	}
)
