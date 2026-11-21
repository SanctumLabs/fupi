package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetDateRangeDescription(t *testing.T) {
	startDate := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 20, 15, 45, 0, 0, time.UTC)
	zeroTime := time.Time{}

	tests := []struct {
		name     string
		filter   RequestParamsDateRangeOption
		expected string
	}{
		{
			name: "Both dates present",
			filter: RequestParamsDateRangeOption{
				StartDate: startDate,
				EndDate:   endDate,
			},
			expected: "Date filter: from 2024-01-15 to 2024-01-20",
		},
		{
			name: "Start date only",
			filter: RequestParamsDateRangeOption{
				StartDate: startDate,
				EndDate:   zeroTime,
			},
			expected: "Date filter: from 2024-01-15",
		},
		{
			name: "End date only",
			filter: RequestParamsDateRangeOption{
				StartDate: zeroTime,
				EndDate:   endDate,
			},
			expected: "Date filter: to 2024-01-20",
		},
		{
			name: "No dates",
			filter: RequestParamsDateRangeOption{
				StartDate: zeroTime,
				EndDate:   zeroTime,
			},
			expected: "No date filter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetDateRangeDescription(tt.filter)
			assert.Equal(t, tt.expected, result)
		})
	}
}
