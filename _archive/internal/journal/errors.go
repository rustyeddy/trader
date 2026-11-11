package journal

import "errors"

// ErrInvalidRecord reports a Record that fails NewRecord's validation.
var ErrInvalidRecord = errors.New("journal: invalid record")

// ErrClosed reports an operation attempted against a closed Recorder
// or Reader.
var ErrClosed = errors.New("journal: closed")
