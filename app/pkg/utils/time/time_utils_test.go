package timeutils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseIsoTimeToTime(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expected    string // RFC3339 format for comparison
	}{
		{
			name:        "Valid RFC3339 format",
			input:       "2025-05-13T12:01:00Z",
			expectError: false,
			expected:    "2025-05-13T12:01:00Z",
		},
		{
			name:        "Valid RFC3339 with timezone offset",
			input:       "2025-05-13T12:01:00+05:30",
			expectError: false,
			expected:    "2025-05-13T12:01:00+05:30",
		},
		{
			name:        "Valid RFC3339 with negative timezone offset",
			input:       "2025-05-13T12:01:00-07:00",
			expectError: false,
			expected:    "2025-05-13T12:01:00-07:00",
		},
		{
			name:        "Invalid format - custom API format",
			input:       "Tue May 13 2025 12:01:00 GMT+0000 (Coordinated Universal Time)",
			expectError: true,
		},
		{
			name:        "RFC3339Nano format (will parse but lose precision)",
			input:       "2025-05-13T12:01:00.123456789Z",
			expectError: false,
			expected:    "2025-05-13T12:01:00Z", // Nanoseconds are truncated in RFC3339
		},
		{
			name:        "Invalid format - completely wrong",
			input:       "not a time",
			expectError: true,
		},
		{
			name:        "Invalid format - completely wrong",
			input:       "2025/05/13 12:01:00", // Wrong separators
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseIsoTimeToTime(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for input %q, but got none", tt.input)
				}
				if result != nil {
					t.Errorf("Expected nil result when error occurs, got %v", result)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input %q: %v", tt.input, err)
				}
				if result == nil {
					t.Errorf("Expected non-nil result for valid input %q", tt.input)
				} else {
					// Compare the formatted time strings
					if result.Format(time.RFC3339) != tt.expected {
						t.Errorf("Expected %q, got %q", tt.expected, result.Format(time.RFC3339))
					}
				}
			}
		})
	}
}

func TestParseIsoTimeNanoToTime(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expected    string // RFC3339Nano format for comparison
	}{
		{
			name:        "Valid RFC3339Nano format",
			input:       "2025-05-13T12:01:00.123456789Z",
			expectError: false,
			expected:    "2025-05-13T12:01:00.123456789Z",
		},
		{
			name:        "Valid RFC3339Nano with timezone offset",
			input:       "2025-05-13T12:01:00.123+05:30",
			expectError: false,
			expected:    "2025-05-13T12:01:00.123+05:30",
		},
		{
			name:        "Valid RFC3339 without nanoseconds",
			input:       "2025-05-13T12:01:00Z",
			expectError: false,
			expected:    "2025-05-13T12:01:00Z",
		},
		{
			name:        "Invalid format - custom API format",
			input:       "Tue May 13 2025 12:01:00 GMT+0000 (Coordinated Universal Time)",
			expectError: true,
		},
		{
			name:        "Invalid format - completely wrong",
			input:       "not a time",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseIsoTimeNanoToTime(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for input %q, but got none", tt.input)
				}
				if result != nil {
					t.Errorf("Expected nil result when error occurs, got %v", result)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input %q: %v", tt.input, err)
				}
				if result == nil {
					t.Errorf("Expected non-nil result for valid input %q", tt.input)
				} else {
					// Compare the formatted time strings
					if result.Format(time.RFC3339Nano) != tt.expected {
						t.Errorf("Expected %q, got %q", tt.expected, result.Format(time.RFC3339Nano))
					}
				}
			}
		})
	}
}

func TestParseCustomTimeFormat(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expectedUTC string // Expected time in UTC RFC3339 format
	}{
		{
			name:        "Valid custom format with timezone description",
			input:       "Tue May 13 2025 12:01:00 GMT+0000 (Coordinated Universal Time)",
			expectError: false,
			expectedUTC: "2025-05-13T12:01:00Z",
		},
		{
			name:        "Valid custom format without timezone description",
			input:       "Tue May 13 2025 12:01:00 GMT+0000",
			expectError: false,
			expectedUTC: "2025-05-13T12:01:00Z",
		},
		{
			name:        "Valid custom format with different timezone",
			input:       "Wed Jun 15 2025 14:30:45 EST-0500",
			expectError: false,
			expectedUTC: "2025-06-15T19:30:45Z",
		},
		{
			name:        "Valid custom format with positive timezone offset",
			input:       "Thu Jul 20 2025 09:15:30 JST+0900",
			expectError: false,
			expectedUTC: "2025-07-20T00:15:30Z",
		},
		{
			name:        "Invalid format - RFC3339",
			input:       "2025-05-13T12:01:00Z",
			expectError: true,
		},
		{
			name:        "Invalid format - completely wrong",
			input:       "not a time",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseCustomTimeFormat(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for input %q, but got none", tt.input)
				}
				if result != nil {
					t.Errorf("Expected nil result when error occurs, got %v", result)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input %q: %v", tt.input, err)
				}
				if result == nil {
					t.Errorf("Expected non-nil result for valid input %q", tt.input)
				} else {
					// Convert to UTC for comparison
					utcTime := result.UTC()
					if utcTime.Format(time.RFC3339) != tt.expectedUTC {
						t.Errorf("Expected %q, got %q", tt.expectedUTC, utcTime.Format(time.RFC3339))
					}
				}
			}
		})
	}
}

func TestSafelyParseIsoTimeToTime(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string // Expected time in UTC RFC3339 format, empty string if expecting nil
	}{
		{
			name:     "RFC3339 format",
			input:    "2025-05-13T12:01:00Z",
			expected: "2025-05-13T12:01:00Z",
		},
		{
			name:     "RFC3339 with timezone offset",
			input:    "2025-05-13T12:01:00+05:30",
			expected: "2025-05-13T06:31:00Z",
		},
		{
			name:     "RFC3339Nano format",
			input:    "2025-05-13T12:01:00.123456789Z",
			expected: "2025-05-13T12:01:00Z", // Nanoseconds truncated in comparison
		},
		{
			name:     "Custom API format with timezone description",
			input:    "Tue May 13 2025 12:01:00 GMT+0000 (Coordinated Universal Time)",
			expected: "2025-05-13T12:01:00Z",
		},
		{
			name:     "Custom API format without timezone description",
			input:    "Wed Jun 15 2025 14:30:45 EST-0500",
			expected: "2025-06-15T19:30:45Z",
		},
		{
			name:     "Invalid format - should return nil",
			input:    "not a valid time format at all",
			expected: "", // Empty string indicates expecting nil
		},
		{
			name:     "Partial valid format - should return nil",
			input:    "2025-05-13",
			expected: "", // Empty string indicates expecting nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SafelyParseIsoTimeToTime(tt.input)

			if tt.expected == "" {
				// Expecting nil result
				if result != nil {
					t.Errorf("Expected nil result for input %q, got %v", tt.input, result)
				}
			} else {
				// Expecting valid result
				if result == nil {
					t.Errorf("Expected non-nil result for input %q", tt.input)
				} else {
					// Convert to UTC for comparison and truncate nanoseconds for consistent comparison
					utcTime := result.UTC().Truncate(time.Second)
					expectedTime, _ := time.Parse(time.RFC3339, tt.expected)
					expectedUTC := expectedTime.UTC().Truncate(time.Second)

					if !utcTime.Equal(expectedUTC) {
						t.Errorf("Expected %q, got %q", expectedUTC.Format(time.RFC3339), utcTime.Format(time.RFC3339))
					}
				}
			}
		})
	}
}

func TestValidateDateRange(t *testing.T) {
	baseTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	laterTime := time.Date(2024, 1, 20, 10, 30, 0, 0, time.UTC)
	zeroTime := time.Time{}

	type testCase struct {
		name        string
		startDate   time.Time
		endDate     time.Time
		expectError bool
		errorMsg    string
	}

	testCases := []testCase{
		{
			name:        "Both empty start and end date should return an error",
			startDate:   time.Time{},
			endDate:     time.Time{},
			expectError: true,
			errorMsg:    "both start and end dates cannot be empty",
		},
		{
			name:        "Start date should not be after end date",
			startDate:   time.Time{},
			endDate:     time.Time{},
			errorMsg:    "both start and end dates cannot be empty",
			expectError: true,
		},
		{
			name:        "Valid range",
			startDate:   baseTime,
			endDate:     laterTime,
			expectError: false,
		},
		{
			name:        "Same start and end date",
			startDate:   baseTime,
			endDate:     baseTime,
			expectError: false,
		},
		{
			name:        "Start date only",
			startDate:   baseTime,
			endDate:     zeroTime,
			expectError: false,
		},
		{
			name:        "End date only",
			startDate:   zeroTime,
			endDate:     baseTime,
			expectError: false,
		},
		{
			name:        "Both dates zero",
			startDate:   zeroTime,
			endDate:     zeroTime,
			expectError: true,
			errorMsg:    "both start and end dates cannot be empty",
		},
		{
			name:        "Start date after end date",
			startDate:   laterTime,
			endDate:     baseTime,
			expectError: true,
			errorMsg:    "start date",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualErr := ValidateDateRange(tc.startDate, tc.endDate)
			if tc.expectError {
				assert.Error(t, actualErr)
				if tc.errorMsg != "" {
					assert.Contains(t, actualErr.Error(), tc.errorMsg)
				}
			} else {
				assert.NoError(t, actualErr)
			}
		})
	}
}

func TestParseHumanFriendlyDate(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expected    time.Time
	}{
		// ISO 8601 formats
		{
			name:        "RFC3339 with timezone",
			input:       "2024-01-15T10:30:00Z",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:        "RFC3339 with offset",
			input:       "2024-01-15T10:30:00+05:30",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.FixedZone("", 5*3600+30*60)),
		},
		{
			name:        "ISO date with time no timezone",
			input:       "2024-01-15T10:30:00",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:        "Simple date",
			input:       "2024-01-15",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:        "Date with space and time",
			input:       "2024-01-15 10:30:00",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},

		// US format
		{
			name:        "US format MM/DD/YYYY",
			input:       "01/15/2024",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:        "US format with time",
			input:       "01/15/2024 10:30:00",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},

		// European format
		{
			name:        "European format DD/MM/YYYY",
			input:       "15/01/2024",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:        "European format with time",
			input:       "15/01/2024 10:30:00",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},

		// Slash formats
		{
			name:        "YYYY/MM/DD format",
			input:       "2024/01/15",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:        "YYYY/MM/DD with time",
			input:       "2024/01/15 10:30:00",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},

		// Dash formats
		{
			name:        "MM-DD-YYYY format",
			input:       "01-15-2024",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:        "DD-MM-YYYY format",
			input:       "15-01-2024",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:        "Date with time no seconds",
			input:       "2024-01-15 10:30",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		// Compact formats
		{
			name:        "Compact YYYYMMDD format",
			input:       "20240115",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:        "Compact YYYYMMDDHHMMSS format",
			input:       "20240115103000",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:        "Compact YYYYMMDDHHMM format",
			input:       "202401151030",
			expectError: false,
			expected:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},

		// Error cases
		{
			name:        "Empty string",
			input:       "",
			expectError: true,
		},
		{
			name:        "Invalid format",
			input:       "invalid-date",
			expectError: true,
		},
		{
			name:        "Invalid date values",
			input:       "2024-13-45",
			expectError: true,
		},
		{
			name:        "Partial date",
			input:       "2024-01",
			expectError: true,
		},
		{
			name:        "Invalid compact format - too short",
			input:       "2024011",
			expectError: true,
		},
		{
			name:        "Invalid compact format - too long",
			input:       "202401151030001",
			expectError: true,
		},
		{
			name:        "Invalid compact format - mixed chars",
			input:       "2024011A",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualTime, actualErr := ParseHumanFriendlyDate(tt.input)

			if tt.expectError {
				assert.Error(t, actualErr, "Expected error for input: %s", tt.input)
				assert.True(t, actualTime.IsZero(), "Result should be zero time on error")
			} else {
				assert.NoError(t, actualErr, "Unexpected error for input: %s", tt.input)
				assert.Equal(t, tt.expected.Year(), actualTime.Year(), "Year mismatch")
				assert.Equal(t, tt.expected.Month(), actualTime.Month(), "Month mismatch")
				assert.Equal(t, tt.expected.Day(), actualTime.Day(), "Day mismatch")
				assert.Equal(t, tt.expected.Hour(), actualTime.Hour(), "Hour mismatch")
				assert.Equal(t, tt.expected.Minute(), actualTime.Minute(), "Minute mismatch")
			}
		})
	}
}

// Table-driven test for edge cases
func TestParseHumanFriendlyDateEdgeCases(t *testing.T) {
	edgeCases := []struct {
		name     string
		input    string
		hasError bool
	}{
		{"Leading/trailing spaces", "  2024-01-15  ", false},
		{"Leap year", "2024-02-29", false},
		{"Non-leap year", "2023-02-29", true},
		{"End of month", "2024-01-31", false},
		{"Invalid month", "2024-13-01", true},
		{"Invalid day", "2024-01-32", true},
		{"Hour 24", "2024-01-15 24:00:00", true},
		{"Valid hour 23", "2024-01-15 23:59:59", false},
		{"Timezone offset", "2024-01-15T10:30:00+05:30", false},
		{"UTC timezone", "2024-01-15T10:30:00Z", false},
		{"Negative timezone", "2024-01-15T10:30:00-08:00", false},

		// Compact format edge cases
		{"Valid compact YYYYMMDD", "20240115", false},
		{"Invalid compact - month 13", "20241301", true},
		{"Invalid compact - day 32", "20240132", true},
		{"Compact with leap year", "20240229", false},
		{"Compact with non-leap year", "20230229", true},
		{"Valid compact with time", "20240115103000", false},
		{"Invalid compact time - hour 24", "20240115240000", true},
		{"Invalid compact time - minute 60", "20240115106000", true},
		{"Invalid compact time - second 60", "20240115103060", true},
		{"Compact format too short", "2024011", true},
		{"Compact format invalid length", "202401151", true},
		{"Compact format with letters", "2024011A", true},
		{"All zeros compact", "00000000", true},
		{"Year 0000 compact", "00000101", true},
		{"Future year compact", "30240115", false},
	}

	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			actualTime, err := ParseHumanFriendlyDate(tc.input)
			if tc.hasError {
				assert.Error(t, err, "Expected error for: %s, got %v", tc.input, actualTime)
			} else {
				assert.NoError(t, err, "Unexpected error for: %s, got %v", tc.input, actualTime)
			}
		})
	}
}

// Benchmark tests
func BenchmarkParseHumanFriendlyDate(b *testing.B) {
	testCases := []string{
		"2024-01-15",
		"2024-01-15T10:30:00Z",
		"01/15/2024",
		"2024/01/15 10:30:00",
	}

	for b.Loop() {
		for _, testCase := range testCases {
			ParseHumanFriendlyDate(testCase)
		}
	}
}
