package strategy

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortWriter accepts only n bytes of each write and reports no error.
type shortWriter struct{ n int }

func (s shortWriter) Write(p []byte) (int, error) { return min(s.n, len(p)), nil }

func TestMessage_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeMessage(&buf, barsMessage{Bars: []marketdata.Bar{{Instrument: "EURUSD", Volume: 7}}}))
	assert.Equal(t, uint32(buf.Len()-4), binary.BigEndian.Uint32(buf.Bytes()), "the prefix is the body length")

	var got barsMessage
	require.NoError(t, readMessage(&buf, &got))
	require.Len(t, got.Bars, 1)
	assert.Equal(t, "EURUSD", got.Bars[0].Instrument)
	assert.Equal(t, int64(7), got.Bars[0].Volume)
}

func TestWriteMessage_ShortWrite(t *testing.T) {
	err := writeMessage(shortWriter{n: 3}, ordersMessage{})
	assert.ErrorIs(t, err, io.ErrShortWrite, "a write that sends part of a frame is an error, not success")
}

func TestWriteMessage_TooLarge(t *testing.T) {
	big := struct{ Data string }{Data: strings.Repeat("a", maxMessage)}
	assert.ErrorContains(t, writeMessage(io.Discard, big), "exceeds")
}

func TestReadMessage_Errors(t *testing.T) {
	var v ordersMessage
	frameOf := func(body string) []byte {
		return append(binary.BigEndian.AppendUint32(nil, uint32(len(body))), body...)
	}
	t.Run("closed before a message is a clean EOF", func(t *testing.T) {
		assert.ErrorIs(t, readMessage(bytes.NewReader(nil), &v), io.EOF)
	})
	t.Run("closed inside the prefix", func(t *testing.T) {
		assert.ErrorIs(t, readMessage(bytes.NewReader([]byte{0, 0}), &v), io.ErrUnexpectedEOF)
	})
	t.Run("closed inside the body", func(t *testing.T) {
		assert.ErrorIs(t, readMessage(bytes.NewReader(frameOf(`{"orders":[]}`)[:8]), &v), io.ErrUnexpectedEOF)
	})
	t.Run("a body that is not JSON", func(t *testing.T) {
		assert.Error(t, readMessage(bytes.NewReader(frameOf(`nope`)), &v))
	})
	t.Run("a length over the cap is refused before it is read", func(t *testing.T) {
		huge := binary.BigEndian.AppendUint32(nil, maxMessage+1)
		assert.ErrorContains(t, readMessage(bytes.NewReader(huge), &v), "exceeds")
	})
}

func TestOrder_WireForm(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeMessage(&buf, ordersMessage{Orders: []Order{
		{Instrument: "EURUSD", Action: "close"},
		{Instrument: "EURUSD", Action: "open", Direction: "short", Price: num.MustParsePrice("1.1"), StopLoss: num.MustParsePrice("1.11")},
	}}))
	body := buf.String()[4:]
	assert.JSONEq(t, `{"orders":[
		{"instrument":"EURUSD","action":"close"},
		{"instrument":"EURUSD","action":"open","direction":"short","price":"1.1","stop_loss":"1.11"}]}`, body,
		"zero prices and empty words are left out, prices are decimal strings")
}
