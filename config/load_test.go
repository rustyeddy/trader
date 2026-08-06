package config

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type serverConfig struct {
	Host string `default:"0.0.0.0"`
	Port int    `default:"8080"`
}

type appConfig struct {
	Name    string `required:"true"`
	Server  serverConfig
	Timeout time.Duration `default:"5s"`
}

func TestLoadDefaultsOnly(t *testing.T) {
	got, err := Load[appConfig](Options{
		Environ: []string{},
	})
	// Name has no default and is required; expect a required error, but
	// Server/Timeout should still resolve from defaults.
	require.Error(t, err)
	assert.Equal(t, "0.0.0.0", got.Server.Host)
	assert.Equal(t, 8080, got.Server.Port)
	assert.Equal(t, 5*time.Second, got.Timeout)
}

func TestLoadPrecedenceDefaultThenFileThenEnvThenOverride(t *testing.T) {
	type Config struct {
		Value string `default:"default-value"`
	}

	// default only
	got, err := Load[Config](Options{Environ: []string{}})
	require.NoError(t, err)
	assert.Equal(t, "default-value", got.Value)

	// file beats default
	got, err = Load[Config](Options{
		Environ:     []string{},
		FileContent: []byte("value: file-value\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, "file-value", got.Value)

	// env beats file
	got, err = Load[Config](Options{
		Environ:     []string{"VALUE=env-value"},
		FileContent: []byte("value: file-value\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, "env-value", got.Value)

	// override beats env
	got, err = Load[Config](Options{
		Environ:     []string{"VALUE=env-value"},
		FileContent: []byte("value: file-value\n"),
		Overrides:   map[string]string{"value": "override-value"},
	})
	require.NoError(t, err)
	assert.Equal(t, "override-value", got.Value)
}

func TestLoadEnvPrefix(t *testing.T) {
	type Config struct {
		Port int
	}

	got, err := Load[Config](Options{
		EnvPrefix: "TRADER",
		Environ:   []string{"TRADER_PORT=9090"},
	})
	require.NoError(t, err)
	assert.Equal(t, 9090, got.Port)
}

func TestLoadEnvPrefixDoesNotMatchWithoutIt(t *testing.T) {
	type Config struct {
		Port int `default:"1"`
	}

	got, err := Load[Config](Options{
		EnvPrefix: "TRADER",
		Environ:   []string{"PORT=9090"}, // missing the prefix
	})
	require.NoError(t, err)
	assert.Equal(t, 1, got.Port, "an unprefixed env var must not satisfy a prefixed lookup")
}

func TestLoadNestedFieldEnvName(t *testing.T) {
	type Config struct {
		Server serverConfig
	}

	got, err := Load[Config](Options{
		Environ: []string{"SERVER_PORT=3000"},
	})
	require.NoError(t, err)
	assert.Equal(t, 3000, got.Server.Port)
	assert.Equal(t, "0.0.0.0", got.Server.Host, "unset nested field keeps its default")
}

func TestLoadRequiredFieldMissing(t *testing.T) {
	type Config struct {
		APIKey string `required:"true"`
	}

	_, err := Load[Config](Options{Environ: []string{}})
	require.ErrorIs(t, err, ErrRequired)

	var fe *FieldError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, "apikey", fe.Path)
}

func TestLoadRequiredFieldSatisfied(t *testing.T) {
	type Config struct {
		APIKey string `required:"true"`
	}

	got, err := Load[Config](Options{Environ: []string{"APIKEY=secret-key"}})
	require.NoError(t, err)
	assert.Equal(t, "secret-key", got.APIKey)
}

func TestLoadAggregatesMultipleErrors(t *testing.T) {
	type Config struct {
		Port    int           `default:"not-a-number"`
		APIKey  string        `required:"true"`
		Timeout time.Duration `default:"not-a-duration"`
	}

	_, err := Load[Config](Options{Environ: []string{}})
	require.Error(t, err)

	var agg *Error
	require.ErrorAs(t, err, &agg)
	assert.Len(t, agg.Fields, 3, "every problem should be reported, not just the first")
}

func TestLoadInvalidTargetNotAStruct(t *testing.T) {
	_, err := Load[int](Options{Environ: []string{}})
	require.ErrorIs(t, err, ErrInvalidTarget)
}

func TestLoadUnsupportedFieldType(t *testing.T) {
	type Config struct {
		Ch chan int
	}

	_, err := Load[Config](Options{Environ: []string{}})
	require.ErrorIs(t, err, ErrUnsupportedType)
}

type validatedConfig struct {
	Min int
	Max int
}

func (c validatedConfig) Validate() error {
	if c.Min > c.Max {
		return errors.New("min must not exceed max")
	}
	return nil
}

func TestLoadCallsValidate(t *testing.T) {
	_, err := Load[validatedConfig](Options{
		Environ:     []string{},
		FileContent: []byte("min: 10\nmax: 1\n"),
	})
	require.ErrorIs(t, err, ErrValidation)
}

func TestLoadValidatePasses(t *testing.T) {
	got, err := Load[validatedConfig](Options{
		Environ:     []string{},
		FileContent: []byte("min: 1\nmax: 10\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, got.Min)
	assert.Equal(t, 10, got.Max)
}

func TestLoadValidateSkippedWhenFieldErrorsExist(t *testing.T) {
	// Min fails to parse; Validate must not run against a partially decoded
	// struct.
	type Config struct {
		Min int `default:"not-a-number"`
	}
	_, err := Load[Config](Options{Environ: []string{}})
	require.Error(t, err)

	var agg *Error
	require.ErrorAs(t, err, &agg)
	for _, fe := range agg.Fields {
		assert.NotErrorIs(t, fe, ErrValidation)
	}
}

func TestLoadSecretValueRedactedFromFieldError(t *testing.T) {
	type Config struct {
		Password string `default:"true"` // wrong type on purpose: int field below
		Port     int    `secret:"true"`
	}
	_, err := Load[Config](Options{
		Environ: []string{"PORT=super-secret-not-a-number"},
	})
	require.Error(t, err)

	var agg *Error
	require.ErrorAs(t, err, &agg)
	for _, fe := range agg.Fields {
		if fe.Path == "port" {
			assert.Empty(t, fe.Value, "a secret field's raw value must not appear in the error")
			assert.NotContains(t, fe.Error(), "super-secret-not-a-number")
		}
	}
}

func TestLoadUsesRealEnvironmentWhenEnvironNil(t *testing.T) {
	// A single Go field name becomes one lowercased path segment, not split
	// on its internal camelCase word boundaries — see doc.go.
	t.Setenv("TRADER_LOADTESTPORT", "4242")

	type Config struct {
		LoadTestPort int
	}

	got, err := Load[Config](Options{EnvPrefix: "TRADER"})
	require.NoError(t, err)
	assert.Equal(t, 4242, got.LoadTestPort)
}
