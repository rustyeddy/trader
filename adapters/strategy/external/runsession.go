package external

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
	"github.com/rustyeddy/trader/strategy"
)

// DefaultCallbackTimeout is the default per-callback supervision
// timer (ADR-062's own "host owns and enforces the per-bar timer"
// contract, below). A future issue may make this operator-tunable in
// more detail; see WithCallbackTimeout.
const DefaultCallbackTimeout = 30 * time.Second

// DefaultAdmissionTimeout is how long a Handshake'd session waits for
// its own Run stream to open (its own RunOpen) before this host gives
// up on it and frees the session slot for another guest. See
// WithAdmissionTimeout.
const DefaultAdmissionTimeout = 30 * time.Second

// runSession is the host's per-connection state for one guest, from a
// completed Handshake through the lifetime of the guest's own Run
// stream. Exactly one runSession exists per Handshake'd connection —
// v1 assumes one strategy identity per connection (ADR-062's own
// scope statement, strategy.proto's own Run doc comment); grpcServer
// enforces that only one runSession is active on a Host at a time
// (server.go's own Handshake).
//
// A runSession bridges two different call shapes: OnBar/OnFill are
// synchronous Go calls (one in flight at a time, from the runner's own
// single-threaded call discipline) that must block until the guest's
// correlated response arrives on the Run stream, while the Run
// stream's own Recv loop is a single long-lived goroutine reading
// whatever the guest sends, whenever it arrives. sequence numbers are
// the correlation key between the two.
//
// # Callback supervision (ADR-062)
//
// A gRPC context deadline attaches to the whole Run stream, not to
// one bar/fill round trip inside it, so per-callback timeout
// enforcement cannot be "a deadline on the call" — ADR-062's own
// corrected supervision policy instead has the host start an
// application-level timer each time it writes a BarEvent/FillEvent
// and is waiting for that callback's own correlated response
// (awaitResponse, below). If the timer fires first, the session is
// torn down (forceTeardown) and an error is returned from the
// in-process OnBar/OnFill call the runner is waiting on — the runner's
// own existing slow/failed-strategy policy applies from there, exactly
// as it would for an in-process strategy whose OnBar itself returned
// an error. A torn-down session is never silently retried; the guest
// must reconnect and Handshake again.
type runSession struct {
	id              string
	descriptor      strategy.Descriptor
	capabilities    []v1.Capability
	callbackTimeout time.Duration

	streamReady chan struct{} // closed once bindStream succeeds

	mu          sync.Mutex // guards stream and currentView
	stream      v1.StrategyHostService_RunServer
	currentView map[uint64]strategy.View // in-flight OnBar callback sequence -> its frozen View, for GetHistoryBars

	sendMu sync.Mutex // serializes the actual stream.Send call, separate from mu

	seq atomic.Uint64

	pendingMu sync.Mutex
	pending   map[uint64]chan *v1.RunClientMessage

	// teardown carries the reason Run should end this stream: nil for
	// a deliberate, graceful end (Close); non-nil for a forced
	// supervision failure (a fired callback timer). Buffered by one so
	// the first cause to fire always gets recorded even if Run's own
	// select has not reached it yet.
	teardown     chan error
	teardownOnce sync.Once

	closed chan struct{} // closed once the session is torn down
}

func newRunSession(id string, descriptor strategy.Descriptor, capabilities []v1.Capability, callbackTimeout time.Duration) *runSession {
	return &runSession{
		id:              id,
		descriptor:      descriptor,
		capabilities:    capabilities,
		callbackTimeout: callbackTimeout,
		streamReady:     make(chan struct{}),
		currentView:     make(map[uint64]strategy.View),
		pending:         make(map[uint64]chan *v1.RunClientMessage),
		teardown:        make(chan error, 1),
		closed:          make(chan struct{}),
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

// send writes msg down the bound Run stream. The actual stream.Send
// call is serialized by sendMu, held across the call itself (not just
// while reading the stream pointer) — grpc requires concurrent Send
// calls on one stream to be serialized, and Start/OnBar/OnFill/Close
// can all reach here.
func (s *runSession) send(msg *v1.RunServerMessage) error {
	s.mu.Lock()
	stream := s.stream
	s.mu.Unlock()
	if stream == nil {
		return fmt.Errorf("external: session %s: run stream not yet open", s.id)
	}

	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return stream.Send(msg)
}

// awaitResponse registers sequence as awaiting a client response,
// sends msg, and blocks until that response is dispatched, ctx is
// done, the session closes, or this callback's own supervision timer
// fires (see the package doc comment on callback supervision) —
// whichever happens first. view, if non-nil, is exposed to a
// concurrent GetHistoryBars call naming this sequence as its own
// in-flight callback, for the duration of the wait; OnFill callbacks
// must pass nil (GetHistoryBars is scoped to an in-flight BarEvent
// callback only, strategy.proto's own GetHistoryBarsRequest doc
// comment).
//
// send itself runs in its own goroutine so a guest that stops
// reading (stream backpressure blocking Send) cannot itself defeat
// ctx/timer observation — the caller-visible wait is never blocked on
// the network write completing.
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

	var timerCh <-chan time.Time
	if s.callbackTimeout > 0 {
		timer := time.NewTimer(s.callbackTimeout)
		defer timer.Stop()
		timerCh = timer.C
	}

	sendErrCh := make(chan error, 1)
	go func() { sendErrCh <- s.send(msg) }()

	for {
		select {
		case err := <-sendErrCh:
			if err != nil {
				return nil, fmt.Errorf("external: session %s: sending sequence %d: %w", s.id, sequence, err)
			}
			sendErrCh = nil // sent; stop selecting on this case, keep waiting for the response
		case resp := <-ch:
			return resp, nil
		case <-s.closed:
			return nil, fmt.Errorf("external: session %s: closed while awaiting sequence %d", s.id, sequence)
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timerCh:
			err := fmt.Errorf("external: session %s: callback %d exceeded timeout %s: guest treated as unresponsive", s.id, sequence, s.callbackTimeout)
			s.forceTeardown(err)
			return nil, err
		}
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
// corresponding BarEvent was built from. Only ever populated for a
// BarEvent callback (awaitResponse's own doc comment).
func (s *runSession) viewFor(callbackSequence uint64) (strategy.View, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.currentView[callbackSequence]
	return v, ok
}

// forceTeardown records reason (nil for a deliberate, graceful end)
// as this session's own teardown cause, for Run to observe and end
// the stream with, and unblocks anything still in
// awaitResponse/waitStream. Only the first call has any effect; later
// calls (for example a fired callback timer racing a caller-driven
// Close) are no-ops.
func (s *runSession) forceTeardown(reason error) {
	s.teardownOnce.Do(func() {
		s.teardown <- reason
		s.close()
	})
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
