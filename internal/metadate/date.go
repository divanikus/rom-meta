package metadate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Normalize returns a portable calendar date string without inventing precision.
//
// Full dates become YYYY-MM-DD, month-only dates become YYYY-MM, and year-only
// dates stay YYYY. Unrecognized values are returned trimmed so source data is
// not silently discarded.
func Normalize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("2006-01-02")
		}
	}

	if match := isoDatePrefix.FindStringSubmatch(value); len(match) == 4 {
		return normalizeYMD(match[1], match[2], match[3], value)
	}
	if match := isoMonth.FindStringSubmatch(value); len(match) == 3 {
		if validYearMonth(match[1], match[2]) {
			return match[1] + "-" + match[2]
		}
		return value
	}
	if yearOnly.MatchString(value) {
		return value
	}
	if match := monthDayYear.FindStringSubmatch(value); len(match) == 4 {
		return normalizeNamedMonth(match[3], match[1], match[2], value)
	}
	if match := dayMonthYear.FindStringSubmatch(value); len(match) == 4 {
		return normalizeNamedMonth(match[3], match[2], match[1], value)
	}
	if match := monthYear.FindStringSubmatch(value); len(match) == 3 {
		month, ok := monthNumber(match[1])
		if !ok {
			return value
		}
		year, err := strconv.Atoi(match[2])
		if err != nil || year <= 0 {
			return value
		}
		return fmt.Sprintf("%04d-%02d", year, month)
	}
	return value
}

func normalizeNamedMonth(rawYear, rawMonth, rawDay, fallback string) string {
	month, ok := monthNumber(rawMonth)
	if !ok {
		return fallback
	}
	year, err := strconv.Atoi(rawYear)
	if err != nil {
		return fallback
	}
	day, err := strconv.Atoi(rawDay)
	if err != nil {
		return fallback
	}
	if !validDate(year, month, day) {
		return fallback
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

func normalizeYMD(rawYear, rawMonth, rawDay, fallback string) string {
	year, err := strconv.Atoi(rawYear)
	if err != nil {
		return fallback
	}
	month, err := strconv.Atoi(rawMonth)
	if err != nil {
		return fallback
	}
	day, err := strconv.Atoi(rawDay)
	if err != nil {
		return fallback
	}
	if !validDate(year, month, day) {
		return fallback
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

func validYearMonth(rawYear, rawMonth string) bool {
	year, err := strconv.Atoi(rawYear)
	if err != nil || year <= 0 {
		return false
	}
	month, err := strconv.Atoi(rawMonth)
	if err != nil {
		return false
	}
	return month >= 1 && month <= 12
}

func validDate(year, month, day int) bool {
	if year <= 0 || month < 1 || month > 12 || day < 1 {
		return false
	}
	parsed := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return parsed.Year() == year && int(parsed.Month()) == month && parsed.Day() == day
}

func monthNumber(value string) (int, bool) {
	key := strings.ToLower(strings.TrimSpace(value))
	key = strings.TrimSuffix(key, ".")
	if len(key) > 3 {
		key = key[:3]
	}
	month, ok := months[key]
	return month, ok
}

var months = map[string]int{
	"jan": 1,
	"feb": 2,
	"mar": 3,
	"apr": 4,
	"may": 5,
	"jun": 6,
	"jul": 7,
	"aug": 8,
	"sep": 9,
	"oct": 10,
	"nov": 11,
	"dec": 12,
}

var isoDatePrefix = regexp.MustCompile(`^([0-9]{4})-([0-9]{2})-([0-9]{2})(?:[T\s].*)?$`)
var isoMonth = regexp.MustCompile(`^([0-9]{4})-([0-9]{2})$`)
var yearOnly = regexp.MustCompile(`^[0-9]{4}$`)
var monthDayYear = regexp.MustCompile(`(?i)^([a-z]+)\.?\s*([0-9]{1,2})(?:st|nd|rd|th)?\s*,?\s*([0-9]{4})$`)
var dayMonthYear = regexp.MustCompile(`(?i)^([0-9]{1,2})(?:st|nd|rd|th)?\s+([a-z]+)\.?\s*,?\s*([0-9]{4})$`)
var monthYear = regexp.MustCompile(`(?i)^([a-z]+)\.?\s+([0-9]{4})$`)
