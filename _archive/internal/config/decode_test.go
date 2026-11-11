package config

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logLevel is a test-only TextUnmarshaler, standing in for the kind of
// caller-defined enum type (or a num type) config must decode without any
// special-casing beyond the interface.
type logLevel string

func (l *logLevel) UnmarshalText(text []byte) error {
	s := logLevel(text)
	switch s {
	case "debug", "info", "warn", "error":
		*l = s
		return nil
	default:
		return fmt.Errorf("unknown log level %q", text)
	}
}

func addressableOf[T any](t *testing.T, v *T) reflect.Value {
	t.Helper()
	return reflect.ValueOf(v).Elem()
}

func TestDecodeScalarBasicKinds(t *testing.T) {
	var s string
	require.NoError(t, decodeScalar(addressableOf(t, &s), "hello"))
	assert.Equal(t, "hello", s)

	var b bool
	require.NoError(t, decodeScalar(addressableOf(t, &b), "true"))
	assert.True(t, b)

	var i int
	require.NoError(t, decodeScalar(addressableOf(t, &i), "-42"))
	assert.Equal(t, -42, i)

	var i8 int8
	require.NoError(t, decodeScalar(addressableOf(t, &i8), "127"))
	assert.Equal(t, int8(127), i8)

	var u uint
	require.NoError(t, decodeScalar(addressableOf(t, &u), "42"))
	assert.Equal(t, uint(42), u)

	var u16 uint16
	require.NoError(t, decodeScalar(addressableOf(t, &u16), "65535"))
	assert.Equal(t, uint16(65535), u16)

	var f32 float32
	require.NoError(t, decodeScalar(addressableOf(t, &f32), "1.5"))
	assert.Equal(t, float32(1.5), f32)

	var f64 float64
	require.NoError(t, decodeScalar(addressableOf(t, &f64), "1.25"))
	assert.Equal(t, 1.25, f64)
}

func TestDecodeScalarRejectsMalformed(t *testing.T) {
	var i int
	err := decodeScalar(addressableOf(t, &i), "not-a-number")
	require.Error(t, err)

	var b bool
	err = decodeScalar(addressableOf(t, &b), "not-a-bool")
	require.Error(t, err)
}

func TestDecodeScalarIntOverflow(t *testing.T) {
	var i8 int8
	err := decodeScalar(addressableOf(t, &i8), "128") // one past int8 max
	require.Error(t, err)
}

func TestDecodeScalarDuration(t *testing.T) {
	var d time.Duration
	require.NoError(t, decodeScalar(addressableOf(t, &d), "1h30m"))
	assert.Equal(t, 90*time.Minute, d)
}

func TestDecodeScalarDurationRejectsMalformed(t *testing.T) {
	var d time.Duration
	err := decodeScalar(addressableOf(t, &d), "not-a-duration")
	require.Error(t, err)
}

func TestDecodeScalarURL(t *testing.T) {
	var u url.URL
	require.NoError(t, decodeScalar(addressableOf(t, &u), "https://example.com/path"))
	assert.Equal(t, "https", u.Scheme)
	assert.Equal(t, "example.com", u.Host)
}

func TestDecodeScalarTextUnmarshaler(t *testing.T) {
	var l logLevel
	require.NoError(t, decodeScalar(addressableOf(t, &l), "warn"))
	assert.Equal(t, logLevel("warn"), l)

	err := decodeScalar(addressableOf(t, &l), "bogus")
	require.Error(t, err)
}

func TestDecodeLeafOptionalPointerAllocatesOnlyOnSuccess(t *testing.T) {
	var target struct{ Port *int }
	l := &leaf{Path: "port", Value: reflect.ValueOf(&target).Elem().Field(0)}

	require.NoError(t, decodeLeaf(l, "8080"))
	require.NotNil(t, target.Port)
	assert.Equal(t, 8080, *target.Port)
}

func TestDecodeLeafOptionalPointerLeftNilOnFailure(t *testing.T) {
	var target struct{ Port *int }
	l := &leaf{Path: "port", Value: reflect.ValueOf(&target).Elem().Field(0)}

	err := decodeLeaf(l, "not-a-port")
	require.Error(t, err)
	assert.Nil(t, target.Port, "a failed parse must not leave a pointer to a half-set value")
}

func TestDecodeLeafEnumAccepts(t *testing.T) {
	var target struct{ Level string }
	l := &leaf{Path: "level", Enum: []string{"debug", "info", "warn"}, Value: reflect.ValueOf(&target).Elem().Field(0)}

	require.NoError(t, decodeLeaf(l, "info"))
	assert.Equal(t, "info", target.Level)
}

func TestDecodeLeafEnumRejects(t *testing.T) {
	var target struct{ Level string }
	l := &leaf{Path: "level", Enum: []string{"debug", "info", "warn"}, Value: reflect.ValueOf(&target).Elem().Field(0)}

	err := decodeLeaf(l, "trace")
	require.ErrorIs(t, err, ErrEnum)
}

func TestDecodeLeafEnumOnOptionalPointer(t *testing.T) {
	var target struct{ Level *string }
	l := &leaf{Path: "level", Enum: []string{"debug", "info"}, Value: reflect.ValueOf(&target).Elem().Field(0)}

	err := decodeLeaf(l, "trace")
	require.ErrorIs(t, err, ErrEnum)
	assert.Nil(t, target.Level, "enum rejection must not leave a pointer to an invalid value")
}

func TestIsLeafTypeUnsupportedKinds(t *testing.T) {
	assert.False(t, isLeafType(reflect.TypeOf(make(chan int))))
	assert.False(t, isLeafType(reflect.TypeOf(struct{ X int }{})))
	assert.False(t, isLeafType(reflect.TypeOf(map[string]int{})))
}
