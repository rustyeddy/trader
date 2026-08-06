package config

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type renderConfig struct {
	Name     string
	Port     int
	Password string        `secret:"true"`
	Timeout  time.Duration `default:"5s"`
	Retries  *int
}

func TestRenderSortsAndFormats(t *testing.T) {
	got, err := Load[renderConfig](Options{
		Environ:     []string{},
		FileContent: []byte("name: trader\nport: 8080\npassword: super-secret\n"),
	})
	require.NoError(t, err)

	var sb strings.Builder
	require.NoError(t, Render(&sb, got))
	out := sb.String()

	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 5)

	// Sorted lexicographically by path: name, password, port, retries, timeout.
	assert.Equal(t, "name = trader", lines[0])
	assert.Equal(t, "password = REDACTED", lines[1])
	assert.Equal(t, "port = 8080", lines[2])
	assert.Equal(t, "retries = <unset>", lines[3])
	assert.Equal(t, "timeout = 5s", lines[4])
}

func TestRenderNeverContainsSecretValue(t *testing.T) {
	got, err := Load[renderConfig](Options{
		Environ:     []string{},
		FileContent: []byte("password: correct-horse-battery-staple\n"),
	})
	require.NoError(t, err)

	out := Sprint(got)
	assert.NotContains(t, out, "correct-horse-battery-staple")
	assert.Contains(t, out, "password = REDACTED")
}

func TestRenderAcceptsPointer(t *testing.T) {
	got, err := Load[renderConfig](Options{Environ: []string{}})
	require.NoError(t, err)

	var sb strings.Builder
	require.NoError(t, Render(&sb, &got))
	assert.Contains(t, sb.String(), "timeout = 5s")
}

func TestRenderOptionalFieldSet(t *testing.T) {
	got, err := Load[renderConfig](Options{
		Environ:     []string{},
		FileContent: []byte("retries: 3\n"),
	})
	require.NoError(t, err)

	out := Sprint(got)
	assert.Contains(t, out, "retries = 3")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("disk full")
}

func TestRenderPropagatesWriteError(t *testing.T) {
	got, err := Load[renderConfig](Options{Environ: []string{}})
	require.NoError(t, err)

	err = Render(failingWriter{}, got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

func TestRenderRejectsNonStruct(t *testing.T) {
	err := Render(&strings.Builder{}, 42)
	require.ErrorIs(t, err, ErrInvalidTarget)
}

func TestRenderRejectsNilPointer(t *testing.T) {
	var cfg *renderConfig
	err := Render(&strings.Builder{}, cfg)
	require.ErrorIs(t, err, ErrInvalidTarget)
}

func TestSprintOnInvalidTargetReportsInline(t *testing.T) {
	out := Sprint(42)
	assert.Contains(t, out, "config:")
}

func TestRenderDurationUsesStringer(t *testing.T) {
	got, err := Load[renderConfig](Options{
		Environ:     []string{},
		FileContent: []byte("timeout: 1h30m\n"),
	})
	require.NoError(t, err)

	out := Sprint(got)
	assert.Contains(t, out, "timeout = 1h30m0s")
}

func TestRenderNumTypeUsesTextMarshaler(t *testing.T) {
	type Config struct {
		Price priceLike
	}
	var c Config
	c.Price = priceLike("1.08473")

	out := Sprint(c)
	assert.Contains(t, out, "price = 1.08473")
}

// priceLike is a test-only stand-in for num.Price: a type whose canonical
// text form comes from MarshalText, not from its underlying Go
// representation, so Render must go through the interface rather than
// falling back to a raw kind-based format. It also implements
// UnmarshalText, since that is what makes collectLeaves treat it as a leaf
// type in the first place — real num types implement both.
type priceLike string

func (p priceLike) MarshalText() ([]byte, error) {
	return []byte(p), nil
}

func (p *priceLike) UnmarshalText(text []byte) error {
	*p = priceLike(text)
	return nil
}
