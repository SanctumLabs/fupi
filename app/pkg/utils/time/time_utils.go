package timeutils

import (
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/sanctumlabs/curtz/app/pkg/utils"
)

func ParseIsoTimeToTime(isoTime string) (*time.Time, error) {
	parsedTime, err := time.Parse(time.RFC3339, isoTime)
	if err != nil {
		return nil, err
	}
	return &parsedTime, nil
}

func ParseIsoTimeNanoToTime(isoTime string) (*time.Time, error) {
	parsedTime, err := time.Parse(time.RFC3339Nano, isoTime)
	if err != nil {
		return nil, err
	}
	return &parsedTime, nil
}

func SafelyParseIsoTimeToTime(isoTime string) *time.Time {
	parsedIsoTime, err := ParseIsoTimeToTime(isoTime)
	if err == nil {
		return parsedIsoTime
	}
	// we don't return an error here, just log it and attempt to parse as nanoseconds
	slog.Warn(
		"utils<SafeParseIsoTimeToTime> Failed to parse iso time attempting to parse as nanoseconds",
		"isoTime", isoTime,
		"error", err,
	)

	// attempt to parse as nano time
	parsedIsoTime, err = ParseIsoTimeNanoToTime(isoTime)
	if err == nil {
		return parsedIsoTime
	}
	// we don't return an error here, just log it
	slog.Warn(
		"utils<SafeParseIsoTimeToTime> Failed to parse iso time date as nanoseconds",
		"isoTime", isoTime,
		"error", err,
	)

	// Preprocess: Remove content after first '(' and trim spaces
	trimmedTimeStr := strings.Split(isoTime, "(")[0]
	trimmedTimeStr = strings.TrimSpace(trimmedTimeStr)

	// Define additional layouts to attempt
	additionalLayouts := []string{
		"Mon Jan 02 2006 15:04:05 MST-0700", // Weekday, month, 2-digit day, time with timezone
		"Mon Jan 2 2006 15:04:05 MST-0700",  // Handles 1-digit day
		"Mon Jan 02 2006 15:04:05 GMT-0700", // Explicit GMT timezone
		"Mon Jan 2 2006 15:04:05 GMT-0700",  // GMT with 1-digit day
		"Mon Jan 02 2006 15:04:05 -0700",    // Numeric timezone only
		"Mon Jan 2 2006 15:04:05 -0700",     // Numeric timezone with 1-digit day
	}
	// Try each additional layout
	for _, layout := range additionalLayouts {
		parsedTime, err := time.Parse(layout, trimmedTimeStr)
		if err == nil {
			return &parsedTime
		}
	}

	// All parsing attempts failed
	slog.Error(
		"utils<SafeParseIsoTimeToTime> Failed to parse iso time with all formats",
		"isoTime", isoTime,
	)
	return nil
}

// ParseCustomTimeFormat handles the format: "Tue May 13 2025 12:01:00 GMT+0000 (Coordinated Universal Time)"
func ParseCustomTimeFormat(timeStr string) (*time.Time, error) {
	// Remove the timezone description in parentheses if present
	if idx := strings.Index(timeStr, " ("); idx != -1 {
		timeStr = timeStr[:idx]
	}

	// The format corresponds to: "Mon Jan 2 2006 15:04:05 MST-0700"
	// Our input has "GMT+0000" format which needs to be handled
	layout := "Mon Jan 2 2006 15:04:05 MST-0700"

	// Try parsing with the standard layout first
	parsedTime, err := time.Parse(layout, timeStr)
	if err != nil {
		// If that fails, try replacing GMT with UTC since Go's parser is picky about timezone names
		modifiedTimeStr := strings.Replace(timeStr, "GMT", "UTC", 1)
		parsedTime, err = time.Parse(layout, modifiedTimeStr)
		if err != nil {
			return nil, err
		}
	}
	return &parsedTime, nil
}

// DeadlockRetryConfig configuration for deadlock retry logic
type DeadlockRetryConfig struct {
	MaxRetries    int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	JitterPercent float64
}

// CalculateBackoffDelay calculates exponential backoff with jitter
func CalculateBackoffDelay(attempt int, config DeadlockRetryConfig) time.Duration {
	if attempt <= 0 {
		return config.BaseDelay
	}

	// Exponential backoff: baseDelay * 2^attempt
	delay := time.Duration(float64(config.BaseDelay) * math.Pow(2, float64(attempt-1)))

	// Cap at maximum delay
	if delay > config.MaxDelay {
		delay = config.MaxDelay
	}

	// Add jitter to prevent thundering herd
	if config.JitterPercent > 0 {
		jitter := float64(delay) * config.JitterPercent * (rand.Float64() - 0.5) * 2
		delay += time.Duration(jitter)
	}

	return delay
}

// ParseHumanFriendlyDate parses various human-friendly date formats
func ParseHumanFriendlyDate(dateStr string) (time.Time, error) {
	trimmedDateStr := strings.TrimSpace(dateStr)

	if trimmedDateStr == "" {
		return time.Time{}, fmt.Errorf("empty date string")
	}

	// List of supported date formats in order of preference
	supportedFormats := []string{
		// ISO 8601 formats
		time.RFC3339,           // "2006-01-02T15:04:05Z07:00"
		time.RFC3339Nano,       // "2006-01-02T15:04:05.999999999Z07:00"
		"2006-01-02T15:04:05Z", // "2024-01-15T10:30:00Z"
		"2006-01-02T15:04:05",  // "2024-01-15T10:30:00"
		"2006-01-02 15:04:05",  // "2024-01-15 10:30:00"
		"2006-01-02",           // "2024-01-15"

		// Common alternative formats
		"01/02/2006",          // "01/15/2024" (US format)
		"01/02/2006 15:04:05", // "01/15/2024 10:30:00"
		"02/01/2006",          // "15/01/2024" (European format)
		"02/01/2006 15:04:05", // "15/01/2024 10:30:00"
		"2006/01/02",          // "2024/01/15"
		"2006/01/02 15:04:05", // "2024/01/15 10:30:00"

		// Dash formats
		"01-02-2006",          // "01-15-2024"
		"02-01-2006",          // "15-01-2024"
		"2006-01-02 15:04",    // "2024-01-15 10:30"
		"01-02-2006 15:04:05", // "01-15-2024 10:30:00"

		// Compact formats
		"20060102",       // "20240115" (YYYYMMDD)
		"20060102150405", // "20240115103000" (YYYYMMDDHHMMSS)
		"200601021504",   // "202401151030" (YYYYMMDDHHMM)
	}

	var lastErr error
	for _, format := range supportedFormats {
		if parsedTime, err := time.Parse(format, trimmedDateStr); err == nil {
			if parsedTime.Year() == 0 {
				return time.Time{}, fmt.Errorf("year 0000 is invalid")
			}
			return parsedTime, nil
		} else {
			lastErr = err
		}
	}

	// If no format worked, try to parse as Unix timestamp
	if len(trimmedDateStr) == 10 && utils.IsAllDigits(trimmedDateStr) {
		if timestamp, err := strconv.ParseInt(trimmedDateStr, 10, 64); err == nil {
			t := time.Unix(timestamp, 0)
			if t.Year() == 0 {
				return time.Time{}, fmt.Errorf("year 0000 is invalid")
			}
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date '%s' with any supported format. Last error: %v", trimmedDateStr, lastErr)
}

// ValidateDateRange validates that a date range is logical
func ValidateDateRange(startDate, endDate time.Time) error {
	if startDate.IsZero() && endDate.IsZero() {
		return fmt.Errorf("both start and end dates cannot be empty")
	}

	if !startDate.IsZero() && !endDate.IsZero() && startDate.After(endDate) {
		return fmt.Errorf("start date (%v) cannot be after end date (%v)", startDate, endDate)
	}

	return nil
}

// SimulateRandomDelay simulates a random delay between minDelay and maxDelay
// This is useful for mocking out network latency in unit tests
// and makes the mock behavior more realistic.
func SimulateRandomDelay(minDelay time.Duration, maxDelay time.Duration) {
	randomDelay := minDelay + time.Duration(rand.Int64N(int64(maxDelay-minDelay)))

	time.Sleep(randomDelay)
}
