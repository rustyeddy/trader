package strategy

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// maxMessage caps one message body, so a garbled length prefix cannot ask for
// gigabytes. Both sides read through readMessage, so both are bounded.
const maxMessage = 16 << 20

// writeMessage sends v as one length-prefixed JSON message, in a single write.
func writeMessage(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(body) > maxMessage {
		return fmt.Errorf("message of %d bytes exceeds %d", len(body), maxMessage)
	}
	buf := binary.BigEndian.AppendUint32(make([]byte, 0, 4+len(body)), uint32(len(body)))
	buf = append(buf, body...)
	n, err := w.Write(buf)
	if err == nil && n < len(buf) {
		err = io.ErrShortWrite
	}
	return err
}

// readMessage reads one length-prefixed JSON message into v. It returns io.EOF
// when the peer closed before a message began, and io.ErrUnexpectedEOF when it
// closed in the middle of one.
func readMessage(r io.Reader, v any) error {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if n > maxMessage {
		return fmt.Errorf("message of %d bytes exceeds %d", n, maxMessage)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return err
	}
	return json.Unmarshal(body, v)
}
