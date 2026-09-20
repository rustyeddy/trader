package config

import (
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func leavesOf(t *testing.T, dst any) []*leaf {
	t.Helper()
	v := reflect.ValueOf(dst).Elem()
	leaves, err := collectLeaves(v)
	require.NoError(t, err)
	return leaves
}

func leafByPath(t *testing.T, leaves []*leaf, path string) *leaf {
	t.Helper()
	for _, l := range leaves {
		if l.Path == path {
			return l
		}
	}
	t.Fatalf("no leaf with path %q among %d leaves", path, len(leaves))
	return nil
}

func TestCollectLeavesFlatStruct(t *testing.T) {
	type Config struct {
		Name string
		Port int
	}
	var c Config

	leaves := leavesOf(t, &c)
	require.Len(t, leaves, 2)
	assert.Equal(t, "name", leafByPath(t, leaves, "name").Path)
	assert.Equal(t, "port", leafByPath(t, leaves, "port").Path)
}

func TestCollectLeavesNestedStruct(t *testing.T) {
	type Server struct {
		Host string
		Port int
	}
	type Config struct {
		Server Server
	}
	var c Config

	leaves := leavesOf(t, &c)
	require.Len(t, leaves, 2)
	assert.NotNil(t, leafByPath(t, leaves, "server.host"))
	assert.NotNil(t, leafByPath(t, leaves, "server.port"))
}

func TestCollectLeavesEmbeddedStructFlattens(t *testing.T) {
	type Common struct {
		LogLevel string
	}
	type Config struct {
		Common
		Port int
	}
	var c Config

	leaves := leavesOf(t, &c)
	require.Len(t, leaves, 2)
	assert.NotNil(t, leafByPath(t, leaves, "loglevel"))
	assert.NotNil(t, leafByPath(t, leaves, "port"))
}

func TestCollectLeavesUnexportedFieldsSkipped(t *testing.T) {
	type Config struct {
		Port     int
		internal string //nolint:unused
	}
	var c Config

	leaves := leavesOf(t, &c)
	require.Len(t, leaves, 1)
	assert.Equal(t, "port", leaves[0].Path)
}

func TestCollectLeavesNameTagOverridesSegment(t *testing.T) {
	type Config struct {
		Port int `config:"listen_port"`
	}
	var c Config

	leaves := leavesOf(t, &c)
	require.Len(t, leaves, 1)
	assert.Equal(t, "listen_port", leaves[0].Path)
}

// TestCollectLeavesRejectsMalformedRequiredTag is the regression test for
// silently defaulting an unparseable required/secret tag to false.
func TestCollectLeavesRejectsMalformedRequiredTag(t *testing.T) {
	type Config struct {
		APIKey string `required:"treu"`
	}
	var c Config

	_, err := collectLeaves(reflect.ValueOf(&c).Elem())
	require.ErrorIs(t, err, ErrInvalidTag)
}

// TestCollectLeavesRejectsMalformedSecretTag is the security-relevant half
// of the same bug: a typo'd secret:"treu" used to silently leave Secret
// false, disabling redaction for that field with no signal that it had
// happened.
func TestCollectLeavesRejectsMalformedSecretTag(t *testing.T) {
	type Config struct {
		Password string `secret:"treu"`
	}
	var c Config

	_, err := collectLeaves(reflect.ValueOf(&c).Elem())
	require.ErrorIs(t, err, ErrInvalidTag)
}

func TestCollectLeavesUnsupportedTypeErrors(t *testing.T) {
	type Config struct {
		Ch chan int
	}
	var c Config

	_, err := collectLeaves(reflect.ValueOf(&c).Elem())
	require.ErrorIs(t, err, ErrUnsupportedType)

	var fe *FieldError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, "ch", fe.Path)
}

func TestCollectLeavesPointerToStructIsUnsupported(t *testing.T) {
	type Server struct{ Port int }
	type Config struct {
		Server *Server
	}
	var c Config

	_, err := collectLeaves(reflect.ValueOf(&c).Elem())
	require.ErrorIs(t, err, ErrUnsupportedType)
}

func TestCollectLeavesRecognizesSpecialTypes(t *testing.T) {
	type Config struct {
		Timeout  time.Duration
		Endpoint url.URL
		Callback *url.URL
	}
	var c Config

	leaves := leavesOf(t, &c)
	require.Len(t, leaves, 3)
	assert.NotNil(t, leafByPath(t, leaves, "timeout"))
	assert.NotNil(t, leafByPath(t, leaves, "endpoint"))
	assert.NotNil(t, leafByPath(t, leaves, "callback"))
}

func TestCollectLeavesRecognizesTextUnmarshaler(t *testing.T) {
	type Config struct {
		Level logLevel
	}
	var c Config

	leaves := leavesOf(t, &c)
	require.Len(t, leaves, 1)
	assert.Equal(t, "level", leaves[0].Path)
}

func TestCollectLeavesOptionalScalarPointer(t *testing.T) {
	type Config struct {
		MaxRetries *int
	}
	var c Config

	leaves := leavesOf(t, &c)
	require.Len(t, leaves, 1)
	assert.Nil(t, c.MaxRetries, "collecting leaves must not allocate the optional field")
}

func TestLeafTagsAreCaptured(t *testing.T) {
	type Config struct {
		Level string `config:"log_level" env:"LOG_LEVEL" flag:"level" default:"info" enum:"debug,info,warn" required:"true" secret:"true"`
	}
	var c Config

	leaves := leavesOf(t, &c)
	require.Len(t, leaves, 1)
	l := leaves[0]

	assert.Equal(t, "log_level", l.Path)
	assert.Equal(t, "LOG_LEVEL", l.EnvOverride)
	assert.Equal(t, "level", l.FlagOverride)
	assert.Equal(t, "info", l.Default)
	assert.True(t, l.HasDefault)
	assert.Equal(t, []string{"debug", "info", "warn"}, l.Enum)
	assert.True(t, l.Required)
	assert.True(t, l.Secret)
}

func TestLeafDefaultDistinguishesAbsentFromEmpty(t *testing.T) {
	type Config struct {
		WithDefault    string `default:""`
		WithoutDefault string
	}
	var c Config

	leaves := leavesOf(t, &c)
	assert.True(t, leafByPath(t, leaves, "withdefault").HasDefault)
	assert.False(t, leafByPath(t, leaves, "withoutdefault").HasDefault)
}

func TestEnvNameDerivation(t *testing.T) {
	l := &leaf{Path: "server.port"}
	assert.Equal(t, "SERVER_PORT", l.EnvName(""))
	assert.Equal(t, "TRADER_SERVER_PORT", l.EnvName("TRADER"))
	assert.Equal(t, "TRADER_SERVER_PORT", l.EnvName("trader"), "prefix is uppercased")
}

func TestEnvNameOverrideBypassesPrefix(t *testing.T) {
	l := &leaf{Path: "server.port", EnvOverride: "PORT"}
	assert.Equal(t, "PORT", l.EnvName("TRADER"))
}

func TestFlagNameDerivation(t *testing.T) {
	l := &leaf{Path: "server.port"}
	assert.Equal(t, "server-port", l.FlagName())
}

func TestFlagNameOverride(t *testing.T) {
	l := &leaf{Path: "server.port", FlagOverride: "port"}
	assert.Equal(t, "port", l.FlagName())
}
