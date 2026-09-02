package emacross

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{"slow period zero", Config{FastPeriod: 20, SlowPeriod: 0}, true},
		{"slow period negative", Config{FastPeriod: 20, SlowPeriod: -1}, true},
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

// TestConfig_JSONEncodesSnakeCase proves Config is manifest-safe (PR
// #260 review): a composition root passing it straight through as
// backtest.ManifestParams.StrategyParameters gets the established
// snake_case key convention, not Go's default field-name keys.
func TestConfig_JSONEncodesSnakeCase(t *testing.T) {
	b, err := json.Marshal(Config{FastPeriod: 20, SlowPeriod: 50})
	require.NoError(t, err)
	assert.JSONEq(t, `{"fast_period":20,"slow_period":50}`, string(b))
}
