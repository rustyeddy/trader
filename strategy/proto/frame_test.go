package proto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrame_RoundTripsPayloadsInOrder(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, WriteFrame(&buf, []byte(`{"a":1}`), DefaultMaxFrame))
	require.NoError(t, WriteFrame(&buf, []byte(`{"b":2}`), DefaultMaxFrame))

	first, err := ReadFrame(&buf, DefaultMaxFrame)
	require.NoError(t, err)
	second, err := ReadFrame(&buf, DefaultMaxFrame)
	require.NoError(t, err)
	_, end := ReadFrame(&buf, DefaultMaxFrame)

	assert.Equal(t, `{"a":1}`, string(first))
	assert.Equal(t, `{"b":2}`, string(second))
	assert.ErrorIs(t, end, io.EOF, "a clean end between frames is io.EOF")
}

func TestWriteFrame_PrefixIsFourByteBigEndianLength(t *testing.T) {
	var buf bytes.Buffer

	require.NoError(t, WriteFrame(&buf, []byte("{}"), 10))

	assert.Equal(t, []byte{0, 0, 0, 2, '{', '}'}, buf.Bytes())
}

func TestWriteFrame_RefusesEmptyAndOversizedPayloadsWritingNothing(t *testing.T) {
	for name, payload := range map[string][]byte{"empty": nil, "over the limit": []byte(strings.Repeat("x", 11))} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer

			err := WriteFrame(&buf, payload, 10)

			assert.Equal(t, CodeProtocol, CodeOf(err))
			assert.Zero(t, buf.Len())
		})
	}
}

func TestWriteFrame_AtTheLimitIsAllowed(t *testing.T) {
	require.NoError(t, WriteFrame(io.Discard, []byte(strings.Repeat("x", 10)), 10))
}

func TestWriteFrame_ReturnsTheWritersError(t *testing.T) {
	boom := errors.New("boom")

	err := WriteFrame(failWriter{boom}, []byte("{}"), 10)

	assert.ErrorIs(t, err, boom)
}

type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

func frameOf(n uint32, payload string) []byte {
	b := binary.BigEndian.AppendUint32(nil, n)
	return append(b, payload...)
}

func TestReadFrame_Refusals(t *testing.T) {
	for name, in := range map[string][]byte{
		"zero length":           frameOf(0, ""),
		"over the limit":        frameOf(11, "xxxxxxxxxxx"),
		"header cut short":      {0, 0, 1},
		"payload cut short":     frameOf(5, "ab"),
		"payload missing":       frameOf(5, ""),
		"a length of four gigs": frameOf(0xFFFFFFFF, ""),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ReadFrame(bytes.NewReader(in), 10)

			require.Error(t, err)
			assert.Equal(t, CodeProtocol, CodeOf(err), "%v", err)
		})
	}
}

func TestReadFrame_ARefusedLengthReadsNoPayload(t *testing.T) {
	r := &countingReader{r: bytes.NewReader(frameOf(1<<30, strings.Repeat("x", 100)))}

	_, err := ReadFrame(r, DefaultMaxFrame)

	require.Equal(t, CodeProtocol, CodeOf(err))
	assert.Equal(t, 4, r.n, "only the header was read: a hostile length allocates nothing")
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestReadFrame_ATransportErrorIsNotAProtocolError(t *testing.T) {
	boom := errors.New("connection reset")

	_, err := ReadFrame(io.MultiReader(bytes.NewReader(frameOf(5, "ab")), errReader{boom}), 10)
	_, headErr := ReadFrame(errReader{boom}, 10)

	assert.ErrorIs(t, headErr, boom)
	assert.ErrorIs(t, err, boom)
	assert.Equal(t, CodeInternal, CodeOf(err))
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestWriteFrame_AShortWriteWithNoErrorIsReported(t *testing.T) {
	for _, n := range []int{0, 3, 5} {
		w := &shortWriter{n: n}

		err := WriteFrame(w, []byte("{}"), 10)

		require.ErrorIs(t, err, io.ErrShortWrite, "a writer that took %d of 6 bytes and said nothing", n)
	}
}

func TestWriteFrame_AFullWriteIsNotAShortWrite(t *testing.T) {
	require.NoError(t, WriteFrame(&shortWriter{n: 6}, []byte("{}"), 10))
}

// shortWriter claims to have written n bytes and reports no error, as a broken
// io.Writer might.
type shortWriter struct{ n int }

func (w *shortWriter) Write([]byte) (int, error) { return w.n, nil }
