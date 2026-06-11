package trader

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseToUnix parses a timestamp string and returns a minute-aligned UTC Unix timestamp.
func parseToUnix(s string) (Timestamp, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty timestamp")
	}

	// First try parsing RFC3339 / RFC3339Nano.
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.ParseInLocation(layout, s, estNoDST)
		if err != nil {
			return 0, fmt.Errorf("parse timestamp %q: %w", s, err)
		}
	}
	u := t.UTC().Unix()
	if u%60 != 0 {
		return 0, fmt.Errorf("timestamp not minute-aligned: %q -> %d", s, u)
	}
	return Timestamp(u), nil
}

// fastPrice parses a decimal price string into the internal fixed-point Price value.
func fastPrice(s string) (Price, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty price")
	}

	// "1.035030" → "1035030"
	buf := make([]byte, 0, len(s))
	digits := 0
	dotCount := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= '0' && ch <= '9':
			buf = append(buf, ch)
			digits++
		case ch == '.':
			dotCount++
			if dotCount > 1 {
				return 0, fmt.Errorf("invalid price %q", s)
			}
		case (ch == '+' || ch == '-') && i == 0:
			buf = append(buf, ch)
		default:
			return 0, fmt.Errorf("invalid price %q", s)
		}
	}
	if digits == 0 || len(buf) == 1 && (buf[0] == '+' || buf[0] == '-') {
		return 0, fmt.Errorf("invalid price %q", s)
	}
	v, err := strconv.ParseInt(string(buf), 10, 32)
	if err != nil {
		return 0, err
	}
	return Price(v), nil
}

// bitIsSet reports whether bit i is set in the bitset.
func bitIsSet(bits []uint64, i int) bool {
	return (bits[i>>6] & (uint64(1) << uint(i&63))) != 0
}

// bitSet sets bit i in the bitset.
func bitSet(bits []uint64, i int) {
	bits[i>>6] |= (uint64(1) << uint(i&63))
}
