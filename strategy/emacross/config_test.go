package emacross

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{"reference values", Config{FastPeriod: 20, SlowPeriod: 50}, false},
		{"fast period zero", Config{FastPeriod: 0, SlowPeriod: 50}, true},
		{"fast period negative", Config{FastPeriod: -1, SlowPeriod: 50}, true},
		{"slow equal to fast", Config{FastPeriod: 20, SlowPeriod: 20}, true},
		{"slow less than fast", Config{FastPeriod: 50, SlowPeriod: 20}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
