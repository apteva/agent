package tools

import (
	"fmt"
	"time"
)

type GetTimeTool struct{}

func (t *GetTimeTool) Name() string {
	return "get_time"
}

func (t *GetTimeTool) DisplayName() string {
	return "Get Current Time"
}

func (t *GetTimeTool) Description() string {
	return "Get the current date and time in various formats"
}

func (t *GetTimeTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"timezone": map[string]interface{}{
				"type":        "string",
				"description": "Timezone to get time for (e.g. 'UTC', 'America/New_York'). Defaults to system timezone.",
			},
			"format": map[string]interface{}{
				"type":        "string",
				"description": "Time format: 'rfc3339', 'unix', 'readable'. Defaults to 'readable'.",
				"enum":        []string{"rfc3339", "unix", "readable"},
			},
		},
		"required": []string{},
	}
}

func (t *GetTimeTool) Execute(params map[string]interface{}) (interface{}, error) {
	now := time.Now()

	// Handle timezone
	if tz, ok := params["timezone"].(string); ok && tz != "" {
		location, err := time.LoadLocation(tz)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone '%s': %v", tz, err)
		}
		now = now.In(location)
	}

	// Handle format
	format := "readable"
	if f, ok := params["format"].(string); ok {
		format = f
	}

	result := map[string]interface{}{
		"timezone": now.Location().String(),
	}

	switch format {
	case "rfc3339":
		result["current_time"] = now.Format(time.RFC3339)
		result["format"] = "rfc3339"
	case "unix":
		result["current_time"] = now.Unix()
		result["format"] = "unix"
	case "readable":
		result["current_time"] = now.Format("Monday, January 2, 2006 at 3:04:05 PM MST")
		result["format"] = "readable"
	default:
		return nil, fmt.Errorf("invalid format '%s'. Use 'rfc3339', 'unix', or 'readable'", format)
	}

	// Add additional time info
	result["iso_date"] = now.Format("2006-01-02")
	result["iso_time"] = now.Format("15:04:05")
	result["day_of_week"] = now.Weekday().String()
	result["day_of_year"] = now.YearDay()
	result["week_number"] = getWeekNumber(now)

	return result, nil
}

func getWeekNumber(t time.Time) int {
	_, week := t.ISOWeek()
	return week
}