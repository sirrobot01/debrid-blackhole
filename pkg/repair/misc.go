package repair

import (
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"os"
	"path/filepath"
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

func fileIsSymlinked(file string) bool {
	info, err := os.Lstat(file)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

func getSymlinkTarget(file string) string {
	if fileIsSymlinked(file) {
		target, err := os.Readlink(file)
		if err != nil {
			return ""
		}
		if !filepath.IsAbs(target) {
			dir := filepath.Dir(file)
			target = filepath.Join(dir, target)
		}
		return target
	}
	return ""
}

func fileIsReadable(filePath string) error {
	// First check if file exists and is accessible
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}

	// Try to read the first 1024 bytes
	err = checkFileStart(filePath)
	if err != nil {
		return err
	}

	return nil
}

func checkFileStart(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	// Read first 1kb
	buffer := make([]byte, 1024)
	_, err = f.Read(buffer)
	if err != nil {
		return err
	}
	return nil
}

func collectFiles(media arr.Content) map[string][]arr.ContentFile {
	uniqueParents := make(map[string][]arr.ContentFile)
	files := media.Files
	for _, file := range files {
		target := getSymlinkTarget(file.Path)
		if target != "" {
			file.IsSymlink = true
			dir, f := filepath.Split(target)
			torrentNamePath := filepath.Clean(dir)
			// Set target path folder/file.mkv
			file.TargetPath = f
			uniqueParents[torrentNamePath] = append(uniqueParents[torrentNamePath], file)
		}
	}
	return uniqueParents
}
