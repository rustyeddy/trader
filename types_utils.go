package trader

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseToUnix parses a timestamp string and returns a minute-aligned UTC Unix timestamp.
func parseToUnix(s string) (Timestamp, error) {
	// First try parsing rfc3339
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.ParseInLocation(layout, strings.TrimSpace(s), estNoDST)
		if err != nil {
			return 0, fmt.Errorf("Error parsing date: %w", err)
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
	// "1.035030" → "1035030"
	buf := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '.' {
			buf = append(buf, s[i])
		}
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
