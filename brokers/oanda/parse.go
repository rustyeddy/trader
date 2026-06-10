package oanda

import (
	"fmt"
	"strconv"
	"time"
)

func parseFloatField(field, raw string) (float64, error) {
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s %q: %w", field, raw, err)
	}
	return v, nil
}

func parseOptionalFloatField(field, raw string) (float64, error) {
	if raw == "" {
		return 0, nil
	}
	return parseFloatField(field, raw)
}

func parseIntField(field, raw string) (int64, error) {
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s %q: %w", field, raw, err)
	}
	return v, nil
}

func parseTimeField(field, raw string) (time.Time, error) {
	v, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s %q: %w", field, raw, err)
	}
	return v, nil
}
