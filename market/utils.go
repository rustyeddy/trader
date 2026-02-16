package market

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseToUnix(s string) (int64, error) {
	t, err := time.ParseInLocation(layout, strings.TrimSpace(s), estNoDST)
	if err != nil {
		return 0, err
	}
	u := t.UTC().Unix()
	if u%60 != 0 {
		return 0, fmt.Errorf("timestamp not minute-aligned: %q -> %d", s, u)
	}
	return u, nil
}

func parseEST(s string) (time.Time, error) {
	t, err := time.ParseInLocation(layout, s, estNoDST)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil // normalize immediately
}

func fastPrice(s string) (int32, error) {
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
	return int32(v), nil
}

func bitIsSet(bits []uint64, i int) bool {
	return (bits[i>>6] & (uint64(1) << uint(i&63))) != 0
}
func bitSet(bits []uint64, i int) {
	bits[i>>6] |= (uint64(1) << uint(i&63))
}
