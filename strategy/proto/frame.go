package proto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// DefaultMaxFrame is the largest frame payload, in bytes, unless a host
// configures another (ADR-094 section 2).
const DefaultMaxFrame = 1 << 20

// WriteFrame writes payload as one frame: a 4-byte big-endian length, then the
// payload. A payload that is empty or longer than max is refused before anything
// is written.
func WriteFrame(w io.Writer, payload []byte, max int) error {
	if len(payload) == 0 {
		return errorf(CodeProtocol, "frame is empty")
	}
	if len(payload) > max {
		return errorf(CodeProtocol, "frame of %d bytes is over the limit of %d", len(payload), max)
	}
	buf := make([]byte, 4, 4+len(payload))
	binary.BigEndian.PutUint32(buf, uint32(len(payload)))
	_, err := w.Write(append(buf, payload...))
	return err
}

// ReadFrame reads one frame and returns its payload. It returns io.EOF when the
// stream ends cleanly before a frame, and a protocol *Error for a zero length, a
// length above max (nothing of the payload is read) or a stream that ends inside a
// frame.
func ReadFrame(r io.Reader, max int) ([]byte, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, errorf(CodeProtocol, "stream ended inside a frame header")
		}
		return nil, err // io.EOF between frames, or a transport error
	}
	n := binary.BigEndian.Uint32(head[:])
	switch {
	case n == 0:
		return nil, errorf(CodeProtocol, "frame length is 0")
	case uint64(n) > uint64(max):
		return nil, errorf(CodeProtocol, "frame length %d is over the limit of %d", n, max)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, errorf(CodeProtocol, "stream ended inside a frame: %s", fmt.Sprint(err))
		}
		return nil, err
	}
	return payload, nil
}
