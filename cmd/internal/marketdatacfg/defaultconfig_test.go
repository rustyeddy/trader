package marketdatacfg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain keeps every test off the machine's real /etc/trader/config.yml.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "marketdatacfg-*")
	if err != nil {
		panic(err)
	}
	DefaultConfigPath = filepath.Join(dir, "absent.yml")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func useDefaultConfig(t *testing.T, content string) {
	t.Helper()
	orig := DefaultConfigPath
	DefaultConfigPath = writeTemp(t, "config.yml", content)
	t.Cleanup(func() { DefaultConfigPath = orig })
}

func TestLoad_DefaultConfigFileSuppliesValues(t *testing.T) {
	tok := writeTemp(t, "pat.txt", "  secret-token\n")
	useDefaultConfig(t, "oanda_base_url: https://api.example/v3\nraw_root: /data/raw\noanda_token_file: "+tok+"\nbacktest:\n  ignored: yes\n")

	cfg, err := Load([]string{}, nil)
	require.NoError(t, err)
	assert.Equal(t, "https://api.example/v3", cfg.OANDABaseURL)
	assert.Equal(t, "/data/raw", cfg.RawRoot)
	assert.Equal(t, "secret-token", cfg.OANDAToken, "token read from the file, whitespace trimmed")
}

func TestLoad_PrecedenceFileEnvFlag(t *testing.T) {
	useDefaultConfig(t, "raw_root: /file/raw\nstore_root: /file/store\noanda_base_url: https://file/v3\n")

	cfg, err := Load([]string{"TRADER_RAW_ROOT=/env/raw", "TRADER_STORE_ROOT=/env/store"}, map[string]string{"store-root": "/flag/store"})
	require.NoError(t, err)
	assert.Equal(t, "/env/raw", cfg.RawRoot, "environment beats the file")
	assert.Equal(t, "/flag/store", cfg.StoreRoot, "a flag beats both")
	assert.Equal(t, "https://file/v3", cfg.OANDABaseURL, "file beats the default")
}

func TestLoad_TokenFromEnvironmentBeatsTokenFile(t *testing.T) {
	tok := writeTemp(t, "pat.txt", "from-file")
	cfg, err := Load([]string{"TRADER_OANDA_TOKEN=from-env", "TRADER_OANDA_TOKEN_FILE=" + tok}, nil)
	require.NoError(t, err)
	assert.Equal(t, "from-env", cfg.OANDAToken)
}

func TestLoad_TokenFileTildeExpandsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "oanda"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "oanda", "pat.txt"), []byte("home-token\n"), 0o600))

	cfg, err := Load([]string{"TRADER_OANDA_TOKEN_FILE=~/.config/oanda/pat.txt"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "home-token", cfg.OANDAToken)
}

func TestLoad_TokenFileFailuresNeverLeakContents(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		_, err := Load([]string{"TRADER_OANDA_TOKEN_FILE=" + writeTemp(t, "pat.txt", " \n")}, nil)
		require.ErrorContains(t, err, "oanda_token_file")
		assert.ErrorContains(t, err, "is empty")
	})
	t.Run("missing", func(t *testing.T) {
		_, err := Load([]string{"TRADER_OANDA_TOKEN_FILE=" + filepath.Join(t.TempDir(), "nope")}, nil)
		require.ErrorContains(t, err, "oanda_token_file")
	})
}

func TestLoad_ConfigEnvSelectsFile(t *testing.T) {
	useDefaultConfig(t, "raw_root: /default/raw\n")
	explicit := writeTemp(t, "other.yml", "raw_root: /explicit/raw\n")

	cfg, err := Load([]string{"TRADER_CONFIG=" + explicit}, nil)
	require.NoError(t, err)
	assert.Equal(t, "/explicit/raw", cfg.RawRoot)
}

func TestLoad_MissingExplicitConfigIsAnErrorButMissingDefaultIsNot(t *testing.T) {
	_, err := Load([]string{"TRADER_CONFIG=" + filepath.Join(t.TempDir(), "nope.yml")}, nil)
	require.ErrorContains(t, err, "TRADER_CONFIG")

	_, err = Load([]string{}, nil) // DefaultConfigPath does not exist
	require.NoError(t, err)
}

func TestLoad_CredentialsInTheConfigFileAreRejected(t *testing.T) {
	for _, key := range []string{"oanda_token", "alpaca_key_id", "alpaca_secret_key"} {
		t.Run(key, func(t *testing.T) {
			useDefaultConfig(t, "raw_root: /r\n"+key+": hunter2-value\n")
			_, err := Load([]string{}, nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, key)
			assert.NotContains(t, err.Error(), "hunter2-value", "the value is never echoed")
		})
	}
}

func TestLoad_EnvironmentTokenStillOverridesTokenFile(t *testing.T) {
	tok := writeTemp(t, "pat.txt", "from-file")
	useDefaultConfig(t, "oanda_token_file: "+tok+"\n")

	cfg, err := Load([]string{"TRADER_OANDA_TOKEN=from-env"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "from-env", cfg.OANDAToken)

	cfg, err = Load([]string{}, nil)
	require.NoError(t, err)
	assert.Equal(t, "from-file", cfg.OANDAToken)
}
