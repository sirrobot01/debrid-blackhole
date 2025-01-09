package repair

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseSchedule(schedule string) (time.Duration, error) {
	if schedule == "" {
		return time.Hour, nil // default 60m
	}

	// Check if it's a time-of-day format (HH:MM)
	if strings.Contains(schedule, ":") {
		return parseTimeOfDay(schedule)
	}

	// Otherwise treat as duration interval
	return parseDurationInterval(schedule)
}

func parseTimeOfDay(schedule string) (time.Duration, error) {
	now := time.Now()
	scheduledTime, err := time.Parse("15:04", schedule)
	if err != nil {
		return 0, fmt.Errorf("invalid time format: %s. Use HH:MM in 24-hour format", schedule)
	}

	// Convert scheduled time to today
	scheduleToday := time.Date(
		now.Year(), now.Month(), now.Day(),
		scheduledTime.Hour(), scheduledTime.Minute(), 0, 0,
		now.Location(),
	)

	if scheduleToday.Before(now) {
		scheduleToday = scheduleToday.Add(24 * time.Hour)
	}

	return scheduleToday.Sub(now), nil
}

func parseDurationInterval(interval string) (time.Duration, error) {
	if len(interval) < 2 {
		return 0, fmt.Errorf("invalid interval format: %s", interval)
	}

	numStr := interval[:len(interval)-1]
	unit := interval[len(interval)-1]

	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid number in interval: %s", numStr)
	}

	switch unit {
	case 'm':
		return time.Duration(num) * time.Minute, nil
	case 'h':
		return time.Duration(num) * time.Hour, nil
	case 'd':
		return time.Duration(num) * 24 * time.Hour, nil
	case 's':
		return time.Duration(num) * time.Second, nil
	default:
		return 0, fmt.Errorf("invalid unit in interval: %c", unit)
	}
}
