package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trader.yml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadConfig_ReadsTheNestedMarketdataBlock(t *testing.T) {
	path := writeConfig(t, "marketdata:\n  root: /somewhere\n  provider: oanda\n  base_url: https://example.test\n  instruments: [EURUSD, GBPUSD]\n  interval: h1\n")

	cfg, err := loadConfig(path, true)

	require.NoError(t, err)
	assert.Equal(t, marketdataConfig{Root: "/somewhere", Provider: "oanda", BaseURL: "https://example.test", Instruments: []string{"EURUSD", "GBPUSD"}, Interval: "h1"}, cfg.Marketdata)
}

func TestLoadConfig_AMissingFileIsFineUnlessNamed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yml")

	cfg, err := loadConfig(missing, false)
	require.NoError(t, err)
	assert.Equal(t, fileConfig{}, cfg)

	_, err = loadConfig(missing, true)
	require.Error(t, err, "a file the operator named must exist")
}

func TestLoadConfig_AnEmptyFileIsEmptyConfig(t *testing.T) {
	cfg, err := loadConfig(writeConfig(t, ""), true)

	require.NoError(t, err)
	assert.Equal(t, fileConfig{}, cfg)
}

func TestLoadConfig_RefusesASecretKeyAndNeverEchoesItsValue(t *testing.T) {
	for name, body := range map[string]string{
		"token":         "marketdata:\n  token: hunter2-VALUE\n",
		"nested secret": "marketdata:\n  root: /x\n  oanda:\n    api_key: hunter2-VALUE\n",
		"top level":     "oanda_token: hunter2-VALUE\n",
		"in a list":     "marketdata:\n  instruments:\n    - password: hunter2-VALUE\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadConfig(writeConfig(t, body), true)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "looks like a secret")
			assert.NotContains(t, err.Error(), "hunter2-VALUE", "the value is never repeated")
		})
	}
}

func TestLoadConfig_RejectsUnknownKeysAndBadYAML(t *testing.T) {
	for name, body := range map[string]string{
		"unknown key": "marketdata:\n  rooot: /x\n",
		"bad yaml":    "marketdata: [unterminated\n",
		"wrong type":  "marketdata:\n  instruments: EURUSD\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadConfig(writeConfig(t, body), true)

			require.Error(t, err)
		})
	}
}

func TestResolveRoot_FlagThenEnvironmentThenFile(t *testing.T) {
	for _, tc := range []struct {
		name            string
		flag, env, file string
		want            string
	}{
		{"flag wins", "/flag", "/env", "/file", "/flag"},
		{"env beats file", "", "/env", "/file", "/env"},
		{"file last", "", "", "/file", "/file"},
		{"blank values are not set", "  ", "", "/file", "/file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveRoot(tc.flag, tc.env, tc.file)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveRoot_NoSourceNamesAllThree(t *testing.T) {
	_, err := resolveRoot("", "", "")

	require.Error(t, err)
	for _, want := range []string{"--data-root", "TRADER_DATA_ROOT", "marketdata.root"} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestLoadConfig_ErrorsNeverQuoteAValue(t *testing.T) {
	for name, body := range map[string]string{
		"tagged secret":     "marketdata: {token: !!int hunter2-VALUE}\n",
		"wrong typed value": "marketdata:\n  instruments: hunter2-VALUE\n",
		"wrong typed root":  "marketdata:\n  root: [hunter2-VALUE]\n",
		"bad tag":           "marketdata:\n  root: !!int hunter2-VALUE\n",
		"syntax":            "marketdata: [hunter2-VALUE\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadConfig(writeConfig(t, body), true)

			require.Error(t, err)
			assert.NotContains(t, err.Error(), "hunter2-VALUE")
		})
	}
}

func TestLoadConfig_NamesTheLineAndTheUnknownKeyButNotTheValue(t *testing.T) {
	_, err := loadConfig(writeConfig(t, "marketdata:\n  rooot: hunter2-VALUE\n"), true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown key "rooot"`)
	assert.Contains(t, err.Error(), "line 2")
	assert.NotContains(t, err.Error(), "hunter2-VALUE")
}

func TestLoadConfig_RejectsASecondDocument(t *testing.T) {
	for name, body := range map[string]string{
		"trailing secret":  "marketdata:\n  root: /x\n---\nmarketdata:\n  token: hunter2-VALUE\n",
		"trailing unknown": "marketdata:\n  root: /x\n---\nsurprise: 1\n",
		"trailing empty":   "marketdata:\n  root: /x\n---\n",
		"broken second":    "marketdata:\n  root: /x\n---\n[unterminated hunter2-VALUE\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadConfig(writeConfig(t, body), true)

			require.Error(t, err)
			assert.NotContains(t, err.Error(), "hunter2-VALUE")
		})
	}
}
