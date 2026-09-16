package external

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"

	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
	"github.com/rustyeddy/trader/strategy"
)

// runSession is the host's per-connection state for one guest, from a
// completed Handshake through the lifetime of the guest's own Run
// stream. Exactly one runSession exists per Handshake'd connection —
// v1 assumes one strategy identity per connection (ADR-062's own
// scope statement, strategy.proto's own Run doc comment).
//
// A runSession bridges two different call shapes: OnBar/OnFill are
// synchronous Go calls (one in flight at a time, from the runner's own
// single-threaded call discipline) that must block until the guest's
// correlated response arrives on the Run stream, while the Run
// stream's own Recv loop is a single long-lived goroutine reading
// whatever the guest sends, whenever it arrives. sequence numbers are
// the correlation key between the two.
type runSession struct {
	id           string
	descriptor   strategy.Descriptor
	capabilities []v1.Capability

	streamReady chan struct{} // closed once bindStream succeeds

	mu          sync.Mutex // guards stream and currentView
	stream      v1.StrategyHostService_RunServer
	currentView map[uint64]strategy.View // in-flight callback sequence -> its frozen View, for GetHistoryBars

	seq atomic.Uint64

	pendingMu sync.Mutex
	pending   map[uint64]chan *v1.RunClientMessage

	closed chan struct{} // closed once the Run stream ends
}

func newRunSession(id string, descriptor strategy.Descriptor, capabilities []v1.Capability) *runSession {
	return &runSession{
		id:           id,
		descriptor:   descriptor,
		capabilities: capabilities,
		streamReady:  make(chan struct{}),
		currentView:  make(map[uint64]strategy.View),
		pending:      make(map[uint64]chan *v1.RunClientMessage),
		closed:       make(chan struct{}),
	}
}

// newSessionID returns a fresh, opaque session token — not one of
// Trader's own id.ID kinds, since a session token is a transport-level
// connection handle, not a domain identifier with its own creation
// timestamp or kind vocabulary.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("external: generating session id: %w", err)
	}
	return "sess_" + hex.EncodeToString(b[:]), nil
}

// bindStream binds stream to this session, called once by Run after
// validating the guest's own RunOpen names this session. Calling it
// twice (a guest that opens Run more than once for one Handshake) is
// rejected — v1's own "exactly one Run stream per Handshake'd
// connection" scope statement.
func (s *runSession) bindStream(stream v1.StrategyHostService_RunServer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stream != nil {
		return fmt.Errorf("external: session %s: run stream already bound", s.id)
	}
	s.stream = stream
	close(s.streamReady)
	return nil
}

// waitStream blocks until bindStream has succeeded, the session
// closes, or ctx is done — whichever happens first.
func (s *runSession) waitStream(ctx context.Context) error {
	select {
	case <-s.streamReady:
		return nil
	case <-s.closed:
		return fmt.Errorf("external: session %s: closed before run stream opened", s.id)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *runSession) nextSequence() uint64 {
	return s.seq.Add(1)
}

// send writes msg down the bound Run stream, serialized against any
// other concurrent send (grpc requires Send calls on one stream to be
// serialized; OnBar/OnFill are already only ever called one at a time
// by a well-behaved runner, but this does not rely on that alone).
func (s *runSession) send(msg *v1.RunServerMessage) error {
	s.mu.Lock()
	stream := s.stream
	s.mu.Unlock()
	if stream == nil {
		return fmt.Errorf("external: session %s: run stream not yet open", s.id)
	}
	return stream.Send(msg)
}

// awaitResponse registers sequence as awaiting a client response,
// sends msg, and blocks until that response is dispatched, ctx is
// done, or the session closes (the Run stream's own Recv loop
// exited, meaning no response will ever arrive). view, if non-nil, is
// exposed to a concurrent GetHistoryBars call naming this sequence as
// its own in-flight callback, for the duration of the wait.
func (s *runSession) awaitResponse(ctx context.Context, sequence uint64, msg *v1.RunServerMessage, view strategy.View) (*v1.RunClientMessage, error) {
	ch := make(chan *v1.RunClientMessage, 1)

	s.pendingMu.Lock()
	s.pending[sequence] = ch
	s.pendingMu.Unlock()

	if view != nil {
		s.mu.Lock()
		s.currentView[sequence] = view
		s.mu.Unlock()
	}

	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, sequence)
		s.pendingMu.Unlock()
		s.mu.Lock()
		delete(s.currentView, sequence)
		s.mu.Unlock()
	}()

	if err := s.send(msg); err != nil {
		return nil, fmt.Errorf("external: session %s: sending sequence %d: %w", s.id, sequence, err)
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-s.closed:
		return nil, fmt.Errorf("external: session %s: closed while awaiting sequence %d", s.id, sequence)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// dispatch delivers a received client message to the sequence it
// responds to, if anything is still waiting for it. A response
// naming a sequence nothing is waiting for (already timed out, or a
// duplicate) is silently dropped, matching the at-least-once
// delivery assumptions the architecture document's own "Delivery
// Semantics" section states for process boundaries generally, rather
// than treated as a protocol violation.
func (s *runSession) dispatch(sequence uint64, msg *v1.RunClientMessage) {
	s.pendingMu.Lock()
	ch, ok := s.pending[sequence]
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

// viewFor returns the in-flight View for callbackSequence, if any —
// used by GetHistoryBars to answer against the exact frozen view the
// corresponding BarEvent/FillEvent was built from.
func (s *runSession) viewFor(callbackSequence uint64) (strategy.View, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.currentView[callbackSequence]
	return v, ok
}

// close marks the session as no longer able to deliver responses,
// unblocking anything still in awaitResponse/waitStream. Safe to call
// more than once.
func (s *runSession) close() {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
}
