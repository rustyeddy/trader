package external

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
// # Design: one goroutine owns all session state
//
// runSession is a small actor, not a mutex-guarded struct (review
// finding, PR #390: the original mutex/atomic/sync.Once choreography
// around pending callbacks, the in-flight frozen View, outbound
// message ordering, the callback timer, and teardown all moved
// together, which is exactly the shape an actor suits better than
// scattered locks). newRunSession starts exactly one goroutine, run,
// that is the *only* goroutine that ever reads or writes this
// session's mutable state; every other goroutine (OnBar/OnFill
// callers, the Run stream's own recv loop, GetHistoryBars) interacts
// with it exclusively through the request/reply channels below —
// bind, waitStream, sysSend, submit, dispatch, history, forceTeardown
// — never a field directly. A second goroutine, the writer started
// once bind succeeds, is the only goroutine that ever calls the bound
// stream's own Send: run hands it outbound messages one at a time
// over writeCh, which is what actually serializes concurrent
// Start/OnBar/OnFill/Close sends onto one stream (gRPC requires
// this), without a send mutex.
//
// # Callback supervision (ADR-062) and cancellation (review finding)
//
// A gRPC context deadline attaches to the whole Run stream, not to
// one bar/fill round trip inside it, so per-callback timeout
// enforcement cannot be "a deadline on the call" — ADR-062's own
// corrected supervision policy instead has the host start an
// application-level timer each time it writes a BarEvent/FillEvent
// and is waiting for that callback's own correlated response (run's
// own handleSubmit, below). If the timer fires first, or the
// caller's own ctx is done first, the session is torn down and an
// error is returned from the in-process OnBar/OnFill call the runner
// is waiting on — the runner's own existing slow/failed-strategy
// policy applies from there, exactly as it would for an in-process
// strategy whose OnBar itself returned an error.
//
// Both the timer and ctx.Done() are treated as terminal for the whole
// session, not just the one call (review discussion on PR #390): once
// a callback's outbound send races a caller giving up on it, the host
// can no longer know whether that BarEvent/FillEvent actually reached
// the guest, and v1 has no protocol mechanism to retract or
// re-synchronize one sequence and safely continue the same session.
// A torn-down session is never silently retried; the guest must
// reconnect and perform a fresh Handshake to start a new session.
type runSession struct {
	id              string
	descriptor      strategy.Descriptor
	capabilities    []v1.Capability
	callbackTimeout time.Duration

	// streamReady is closed by run, exactly once, the instant bind
	// succeeds — a plain one-shot broadcast, not session-owned mutable
	// state in the sense the rest of this doc comment means: once
	// closed it never changes again, so any number of goroutines may
	// safely read it without going through run at all.
	streamReady chan struct{}

	bindCh     chan bindRequest
	sysSendCh  chan sysSendRequest
	submitCh   chan submitRequest
	dispatchCh chan dispatchRequest
	historyCh  chan historyRequest
	teardownCh chan error

	// done is closed by run exactly once, immediately before it
	// returns. endReason is written exactly once, by run, strictly
	// before done is closed — the channel close is what makes reading
	// endReason after <-done safe without any lock (a channel close
	// happens-before every receive that observes it).
	done      chan struct{}
	endReason error

	// seq is the one exception to "only run touches mutable state":
	// a bare monotonic counter with no coupled invariants, exactly the
	// case an atomic remains the simplest correct tool for (see the
	// package's own review-response reasoning) rather than routing a
	// pure counter increment through the actor's own channels.
	seq atomic.Uint64
}

func newRunSession(id string, descriptor strategy.Descriptor, capabilities []v1.Capability, callbackTimeout time.Duration) *runSession {
	s := &runSession{
		id:              id,
		descriptor:      descriptor,
		capabilities:    capabilities,
		callbackTimeout: callbackTimeout,
		streamReady:     make(chan struct{}),
		bindCh:          make(chan bindRequest),
		sysSendCh:       make(chan sysSendRequest),
		submitCh:        make(chan submitRequest),
		dispatchCh:      make(chan dispatchRequest),
		historyCh:       make(chan historyRequest),
		teardownCh:      make(chan error),
		done:            make(chan struct{}),
	}
	go s.run()
	return s
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

// bindRequest asks run to bind stream as this session's own Run
// stream — sent once by grpcServer.Run after it validates the guest's
// own RunOpen names this session.
type bindRequest struct {
	stream v1.StrategyHostService_RunServer
	reply  chan error
}

// sysSendRequest asks run to send msg down the bound stream and wait
// for that Send call itself to complete — used for messages with no
// correlated response (SessionStart, SessionEnd), unlike submitRequest.
type sysSendRequest struct {
	msg   *v1.RunServerMessage
	reply chan error
}

// submitRequest asks run to send msg (already carrying sequence) and
// wait for the guest's own correlated response, honoring ctx and this
// session's own callback timer concurrently — used for BarEvent/
// FillEvent. view is the in-flight frozen View to answer a concurrent
// GetHistoryBars against; nil for a fill callback (GetHistoryBars is
// scoped to an in-flight BarEvent callback only).
type submitRequest struct {
	ctx      context.Context
	sequence uint64
	msg      *v1.RunServerMessage
	view     strategy.View
	reply    chan submitReply
}

type submitReply struct {
	resp *v1.RunClientMessage
	err  error
}

// dispatchRequest delivers one received client message to run, tagged
// with the sequence it responds to (read directly off the message by
// the caller — grpcServer.Run's own recv loop).
type dispatchRequest struct {
	sequence uint64
	msg      *v1.RunClientMessage
}

// historyRequest asks run whether sequence names the currently
// in-flight BarEvent callback, and if so, for its own frozen View.
type historyRequest struct {
	sequence uint64
	reply    chan historyReply
}

type historyReply struct {
	view strategy.View
	ok   bool
}

// writeRequest is run's own handoff to the dedicated writer goroutine
// (started once bind succeeds) — the only goroutine that ever calls
// stream.Send, which is what actually serializes concurrent senders
// without a mutex.
type writeRequest struct {
	msg    *v1.RunServerMessage
	result chan error
}

func runWriter(stream v1.StrategyHostService_RunServer, writeCh <-chan writeRequest) {
	for req := range writeCh {
		req.result <- stream.Send(req.msg)
	}
}

// run is the session's own single-goroutine event loop — see the
// package doc comment above for why. It returns once this session
// ends, for any reason: a deliberate Close (a nil reason reaching
// teardownCh), a fired callback timer or caller ctx cancellation
// (handleSubmit's own terminal cases), or the Run stream itself ending
// (grpcServer.Run's own recv goroutine forwarding the cause via
// forceTeardown).
func (s *runSession) run() {
	var stream v1.StrategyHostService_RunServer
	var writeCh chan writeRequest

	defer func() {
		if writeCh != nil {
			close(writeCh)
		}
	}()

	end := func(reason error) {
		s.endReason = reason
		close(s.done)
	}

	for {
		select {
		case req := <-s.bindCh:
			if stream != nil {
				req.reply <- fmt.Errorf("external: session %s: run stream already bound", s.id)
				continue
			}
			stream = req.stream
			// Buffered by one: lets the actor hand off the next write
			// without blocking even while the writer is still mid-Send
			// on the previous one — the realistic worst case given
			// this protocol's own synchronous usage (Start's
			// SessionStart completes, including its own wait, before
			// any OnBar begins; Close's SessionEnd is the terminal
			// write). See handleSubmit/handleSysSend, neither of which
			// otherwise block the actor on this hand-off (review
			// finding: an inline `writeCh <- ...; <-result` in the
			// sysSendCh case blocked the whole actor — teardown,
			// dispatch, and history all included — for as long as the
			// guest failed to read, which could deadlock Close/Run's
			// own shutdown entirely).
			writeCh = make(chan writeRequest, 1)
			go runWriter(stream, writeCh)
			close(s.streamReady)
			req.reply <- nil

		case req := <-s.sysSendCh:
			if stream == nil {
				req.reply <- fmt.Errorf("external: session %s: run stream not yet open", s.id)
				continue
			}
			if s.handleSysSend(req, writeCh, end) {
				return
			}

		case req := <-s.submitCh:
			if s.handleSubmit(req, stream, writeCh, end) {
				return
			}

		case hreq := <-s.historyCh:
			// No callback is in flight right now — history is only
			// ever answerable while one is (handleSubmit's own case).
			hreq.reply <- historyReply{ok: false}

		case <-s.dispatchCh:
			// A response naming a sequence nothing is waiting for
			// (already timed out, already answered, or a duplicate) —
			// dropped, matching the at-least-once delivery assumptions
			// the architecture document's own "Delivery Semantics"
			// section states for process boundaries generally.

		case reason := <-s.teardownCh:
			end(reason)
			return
		}
	}
}

// handleSubmit drives one submit request to its own conclusion: it
// hands msg to the writer, then waits for the correlated dispatch, a
// fired callback timer, req.ctx.Done(), or a concurrent teardown —
// whichever happens first — answering any GetHistoryBars naming this
// same callback along the way. It reports whether the whole session
// should now end (true) or run's own outer loop should simply
// continue (false).
func (s *runSession) handleSubmit(req submitRequest, stream v1.StrategyHostService_RunServer, writeCh chan writeRequest, end func(error)) (sessionEnded bool) {
	if stream == nil {
		req.reply <- submitReply{err: fmt.Errorf("external: session %s: run stream not yet open", s.id)}
		return false
	}

	sendResult := make(chan error, 1)
	writeCh <- writeRequest{msg: req.msg, result: sendResult}

	var timerCh <-chan time.Time
	if s.callbackTimeout > 0 {
		timer := time.NewTimer(s.callbackTimeout)
		defer timer.Stop()
		timerCh = timer.C
	}

	for {
		select {
		case err := <-sendResult:
			if err != nil {
				req.reply <- submitReply{err: fmt.Errorf("external: session %s: sending sequence %d: %w", s.id, req.sequence, err)}
				return false
			}
			sendResult = nil // sent; keep waiting for the response below

		case dreq := <-s.dispatchCh:
			if dreq.sequence != req.sequence {
				continue // stale/duplicate; drop and keep waiting for ours
			}
			req.reply <- submitReply{resp: dreq.msg}
			return false

		case hreq := <-s.historyCh:
			if req.view != nil {
				hreq.reply <- historyReply{view: req.view, ok: true}
			} else {
				hreq.reply <- historyReply{ok: false}
			}

		case reason := <-s.teardownCh:
			req.reply <- submitReply{err: fmt.Errorf("external: session %s: closed while awaiting sequence %d", s.id, req.sequence)}
			end(reason)
			return true

		case <-timerCh:
			callbackErr := fmt.Errorf("external: session %s: callback %d exceeded timeout %s: guest treated as unresponsive", s.id, req.sequence, s.callbackTimeout)
			req.reply <- submitReply{err: callbackErr}
			end(status.Error(codes.DeadlineExceeded, callbackErr.Error()))
			return true

		case <-req.ctx.Done():
			ctxErr := req.ctx.Err()
			req.reply <- submitReply{err: ctxErr}
			end(statusFromContextErr(ctxErr))
			return true
		}
	}
}

// handleSysSend drives one system-send request (SessionStart,
// SessionEnd — messages with no correlated response) to its own
// conclusion, exactly like handleSubmit does for a callback: it hands
// msg to the writer and then keeps selecting rather than blocking
// inline on the result (review finding — the earlier inline `writeCh
// <- ...; <-result` blocked the whole actor, including teardown, for
// as long as a stalled guest left the write pending, which could
// deadlock Close/Run's own shutdown). A sysSend has no sequence to
// correlate against, so a dispatch or history request arriving while
// one is in flight is answered as "nothing in flight" rather than
// queued — Start completes (including this same wait) before any
// OnBar begins, and Close's SessionEnd is the terminal write, so
// neither case has a real in-flight callback to conflict with.
func (s *runSession) handleSysSend(req sysSendRequest, writeCh chan writeRequest, end func(error)) (sessionEnded bool) {
	result := make(chan error, 1)
	writeCh <- writeRequest{msg: req.msg, result: result}

	for {
		select {
		case err := <-result:
			req.reply <- err
			return false

		case <-s.dispatchCh:
			// Nothing is in flight for a sysSend; drop, matching
			// handleSubmit/run's own stale-message handling.

		case hreq := <-s.historyCh:
			hreq.reply <- historyReply{ok: false}

		case reason := <-s.teardownCh:
			req.reply <- fmt.Errorf("external: session %s: closed while sending", s.id)
			end(reason)
			return true
		}
	}
}

// statusFromContextErr classifies a context error as the grpc status
// code Run should end the RPC with when a caller's own ctx ends an
// in-flight callback (handleSubmit's own terminal ctx.Done() case).
func statusFromContextErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, err.Error())
	}
	return status.Error(codes.Canceled, err.Error())
}

// bind binds stream to this session, called once by grpcServer.Run
// after validating the guest's own RunOpen names this session.
// Calling it twice (a guest that opens Run more than once for one
// Handshake) is rejected — v1's own "exactly one Run stream per
// Handshake'd connection" scope statement.
func (s *runSession) bind(stream v1.StrategyHostService_RunServer) error {
	reply := make(chan error, 1)
	select {
	case s.bindCh <- bindRequest{stream: stream, reply: reply}:
	case <-s.done:
		return fmt.Errorf("external: session %s: closed", s.id)
	}
	return <-reply
}

// waitStream blocks until bind has succeeded, the session ends, or
// ctx is done — whichever happens first.
func (s *runSession) waitStream(ctx context.Context) error {
	select {
	case <-s.streamReady:
		return nil
	case <-s.done:
		return fmt.Errorf("external: session %s: closed before run stream opened", s.id)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sysSend sends msg down the bound stream and waits for the Send call
// itself to complete, honoring ctx and session end. Used for messages
// with no correlated response (SessionStart, SessionEnd).
func (s *runSession) sysSend(ctx context.Context, msg *v1.RunServerMessage) error {
	reply := make(chan error, 1)
	select {
	case s.sysSendCh <- sysSendRequest{msg: msg, reply: reply}:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return fmt.Errorf("external: session %s: closed", s.id)
	}
	select {
	case err := <-reply:
		return err
	case <-s.done:
		return fmt.Errorf("external: session %s: closed while sending", s.id)
	}
}

// submit sends msg (already carrying sequence) and blocks for the
// guest's own correlated response — see handleSubmit for exactly what
// can end the wait. view is exposed to a concurrent GetHistoryBars
// naming sequence as its own in-flight callback; pass nil for a fill
// callback (GetHistoryBars is scoped to an in-flight BarEvent callback
// only, strategy.proto's own GetHistoryBarsRequest doc comment).
func (s *runSession) submit(ctx context.Context, sequence uint64, msg *v1.RunServerMessage, view strategy.View) (*v1.RunClientMessage, error) {
	reply := make(chan submitReply, 1)
	req := submitRequest{ctx: ctx, sequence: sequence, msg: msg, view: view, reply: reply}
	select {
	case s.submitCh <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("external: session %s: closed", s.id)
	}
	select {
	case r := <-reply:
		return r.resp, r.err
	case <-s.done:
		return nil, fmt.Errorf("external: session %s: closed while awaiting sequence %d", s.id, sequence)
	}
}

// dispatch delivers a received client message to run, tagged with the
// sequence it responds to.
func (s *runSession) dispatch(sequence uint64, msg *v1.RunClientMessage) {
	select {
	case s.dispatchCh <- dispatchRequest{sequence: sequence, msg: msg}:
	case <-s.done:
	}
}

// history asks whether sequence names the currently in-flight
// BarEvent callback, returning its own frozen View if so — used by
// GetHistoryBars.
func (s *runSession) history(sequence uint64) (strategy.View, bool) {
	reply := make(chan historyReply, 1)
	select {
	case s.historyCh <- historyRequest{sequence: sequence, reply: reply}:
	case <-s.done:
		return nil, false
	}
	select {
	case r := <-reply:
		return r.view, r.ok
	case <-s.done:
		return nil, false
	}
}

// forceTeardown asks run to end this session, with reason as exactly
// the value grpcServer.Run should itself return (nil for a
// deliberate, graceful end; an already-classified grpc status error
// otherwise — see handleSubmit and statusFromContextErr for how a
// fired timer or ctx cancellation each produce one). Only the first
// call has any effect — run's own <-s.done guard makes every later
// call a safe no-op.
func (s *runSession) forceTeardown(reason error) {
	select {
	case s.teardownCh <- reason:
	case <-s.done:
	}
}

// nextSequence returns the next monotonically increasing callback
// sequence number for this session.
func (s *runSession) nextSequence() uint64 {
	return s.seq.Add(1)
}
