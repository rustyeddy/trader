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

func SecondsToTFString(sec int32) (string, error) {
	if sec <= 0 {
		return "", fmt.Errorf("invalid timeframe seconds: %d", sec)
	}

	// Minutes
	if sec < 3600 && sec%60 == 0 {
		return fmt.Sprintf("M%d", sec/60), nil
	}

	// Hours
	if sec < 86400 && sec%3600 == 0 {
		return fmt.Sprintf("H%d", sec/3600), nil
	}

	// Days
	if sec%86400 == 0 {
		days := sec / 86400
		if days == 7 {
			return "W1", nil
		}
		if days == 30 {
			return "MN1", nil
		}
		return fmt.Sprintf("D%d", days), nil
	}

	return "", fmt.Errorf("cannot map timeframe: %d seconds", sec)
}

func TFStringToSeconds(tf string) (int32, error) {
	switch tf {
	case "M1":
		return 60, nil
	case "M5":
		return 300, nil
	case "M15":
		return 900, nil
	case "M30":
		return 1800, nil
	case "H1":
		return 3600, nil
	case "H4":
		return 14400, nil
	case "D1":
		return 86400, nil
	case "W1":
		return 604800, nil
	case "MN1":
		return 2592000, nil
	default:
		return 0, fmt.Errorf("unsupported timeframe string: %s", tf)
	}
}
