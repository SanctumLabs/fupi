package common

import "time"

// SortOrder to use when sorting records
type (
	SortOrder string

	// DateField is the date field the query should use to filter
	DateField string

	// OrderBy is the field to order collections by
	OrderBy string
)

const (
	SortOderAsc   SortOrder = "ASC"
	SortOrderDesc SortOrder = "DESC"

	OrderByCreatedAt OrderBy = "created_at"
	OrderByUpdatedAt OrderBy = "updated_at"
	OrderByDeletedAt OrderBy = "deleted_at"
	OrderByAmount    OrderBy = "amount"

	DateFieldCreatedAt DateField = "created_at"
	DateFieldUpdatedAt DateField = "updated_at"
	DateFieldDeletedAt DateField = "deleted_at"
)

// RequestParamsOrderOption contains filtering values to use for ordering
type (
	RequestParamsOrderOption struct {
		// OrderBy is the field to order records by
		OrderBy OrderBy

		// SortOrder is the order to sort the records by
		SortOrder SortOrder

		// AmountSortOrder contains sorting order for amount fields
		AmountSortOrder SortOrder
	}

	// RequestParamsOrderOption contains filtering values to use for ordering
	RequestParamsDateRangeOption struct {
		// StartDate is the start date is the start of the date range
		StartDate time.Time

		// EndDate is the end date of the date range
		EndDate time.Time

		// DateFieldOption is the date field the query should use to filter the range on. So, if set to `created_at`, the start and end dates will be based on the
		// `created_at` field of records
		DateFieldOption DateField
	}
)
