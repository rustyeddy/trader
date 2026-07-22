package brokers

import "testing"

func TestIsKnownBroker(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"oanda", true},
		{"OANDA", false}, // case-sensitive by design; callers normalize if needed
		{"", false},
		{"alpaca", false},
	}
	for _, tc := range cases {
		if got := IsKnownBroker(tc.name); got != tc.want {
			t.Errorf("IsKnownBroker(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
