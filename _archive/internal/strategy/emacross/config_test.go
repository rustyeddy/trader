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
		{"allowed side both", Config{FastPeriod: 20, SlowPeriod: 50, AllowedSide: SideBoth}, false},
		{"allowed side long-only", Config{FastPeriod: 20, SlowPeriod: 50, AllowedSide: SideLongOnly}, false},
		{"allowed side short-only", Config{FastPeriod: 20, SlowPeriod: 50, AllowedSide: SideShortOnly}, false},
		{"allowed side invalid", Config{FastPeriod: 20, SlowPeriod: 50, AllowedSide: Side(99)}, true},
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
	assert.JSONEq(t, `{"fast_period":20,"slow_period":50,"allowed_side":"both"}`, string(b))
}

func TestSide_String(t *testing.T) {
	assert.Equal(t, "both", SideBoth.String())
	assert.Equal(t, "long-only", SideLongOnly.String())
	assert.Equal(t, "short-only", SideShortOnly.String())
	assert.Equal(t, "Side(99)", Side(99).String())
}

func TestSide_MarshalText(t *testing.T) {
	for _, s := range []Side{SideBoth, SideLongOnly, SideShortOnly} {
		text, err := s.MarshalText()
		require.NoError(t, err)
		assert.Equal(t, s.String(), string(text))
	}

	_, err := Side(99).MarshalText()
	assert.Error(t, err)
}

func TestSide_UnmarshalText(t *testing.T) {
	cases := map[string]Side{
		"both":       SideBoth,
		"":           SideBoth,
		"long-only":  SideLongOnly,
		"short-only": SideShortOnly,
	}
	for text, want := range cases {
		var s Side
		require.NoError(t, s.UnmarshalText([]byte(text)))
		assert.Equal(t, want, s)
	}

	var s Side
	assert.Error(t, s.UnmarshalText([]byte("sideways")))
}

// TestConfig_JSONEncodesAllowedSideByMode proves AllowedSide serializes
// as a readable string in every mode, not just the zero-value default
// TestConfig_JSONEncodesSnakeCase already covers.
func TestConfig_JSONEncodesAllowedSideByMode(t *testing.T) {
	cases := map[Side]string{
		SideBoth:      "both",
		SideLongOnly:  "long-only",
		SideShortOnly: "short-only",
	}
	for side, want := range cases {
		b, err := json.Marshal(Config{FastPeriod: 20, SlowPeriod: 50, AllowedSide: side})
		require.NoError(t, err)
		assert.JSONEq(t, `{"fast_period":20,"slow_period":50,"allowed_side":"`+want+`"}`, string(b))
	}
}
