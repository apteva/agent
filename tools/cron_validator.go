package tools

import (
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"
)

// ValidateRecurrence validates a recurrence pattern (simple or cron expression)
func ValidateRecurrence(recurrence string) error {
	if recurrence == "" {
		return nil // One-time task
	}

	// Check simple patterns first
	if recurrence == "daily" || recurrence == "weekly" || recurrence == "monthly" {
		return nil
	}

	// Try to parse as cron expression
	if isCronExpression(recurrence) {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		_, err := parser.Parse(recurrence)
		if err != nil {
			return fmt.Errorf("invalid cron expression '%s': %w", recurrence, err)
		}
		return nil
	}

	return fmt.Errorf("invalid recurrence pattern '%s'. Must be 'daily', 'weekly', 'monthly', or a cron expression (e.g., '*/5 * * * *')", recurrence)
}

// IsCronExpression checks if recurrence is a cron expression
func IsCronExpression(recurrence string) bool {
	// Simple patterns are not cron
	if recurrence == "daily" || recurrence == "weekly" || recurrence == "monthly" {
		return false
	}
	return isCronExpression(recurrence)
}

// isCronExpression checks if a string looks like a cron expression
// A valid cron expression has 5 fields separated by spaces
func isCronExpression(s string) bool {
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return false
	}

	// Valid cron characters: digits, asterisk, slash, dash, comma
	cronChars := "0123456789*/-,"
	for _, field := range fields {
		if field == "" {
			return false
		}
		for _, char := range field {
			if !strings.ContainsRune(cronChars, char) {
				return false
			}
		}
	}

	return true
}
