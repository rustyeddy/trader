package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/rustyeddy/trader/strategy/proto"
)

// Errors of a run.
var (
	// ErrClosed is returned by a Session that has ended: finished, failed, or
	// dropped. Nothing more can be exchanged.
	ErrClosed = errors.New("the run has ended")
	// ErrDisconnected is returned when the worker's connection ends mid-run. The
	// run is failed closed; what was recorded before is kept.
	ErrDisconnected = errors.New("worker disconnected")
)

// WorkerError is returned when the worker itself ends the run with an error
// message.
type WorkerError struct{ Failure *proto.Failure }

func (e *WorkerError) Error() string {
	return fmt.Sprintf("worker ended the run: %s: %s", e.Failure.Code, e.Failure.Message)
}

// Session is one admitted run over one connection. It carries the RunID and
// drives the strict lock-step: one bars message out, one orders message back. It
// is used by one goroutine at a time (the Handler's).
type Session struct {
	conn  net.Conn
	cfg   Config
	log   *slog.Logger
	runID string
	reg   *proto.Register
	grant Grant

	mu       sync.Mutex
	closed   bool
	outcome  string
	trSeq    uint64 // next seq Trader sends
	wkSeq    uint64 // next seq expected from the worker
	instrSet map[string]bool
}

func newSession(conn net.Conn, cfg Config, runID string, reg *proto.Register, grant Grant, log *slog.Logger) *Session {
	set := map[string]bool{}
	for _, in := range grant.Instruments {
		set[in] = true
	}
	// accepted was seq 1; register was the worker's seq 1.
	return &Session{conn: conn, cfg: cfg, log: log, runID: runID, reg: reg, grant: grant, trSeq: 2, wkSeq: 2, instrSet: set}
}

// RunID is the unique id of the run. It is on every message after accepted.
func (s *Session) RunID() string { return s.runID }

// Register is what the worker registered: its name, build, config and run.
func (s *Session) Register() *proto.Register { return s.reg }

// Grant is what the host granted at admission.
func (s *Session) Grant() Grant { return s.grant }

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// status is how the run ended, for the log: "" while it is open.
func (s *Session) status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.outcome
}

// Exchange sends one bars message and returns the worker's orders for it. It
// stamps the header (version, type, seq, RunID), so the caller sets only FrameAt,
// Warm and Bars. The reply must be an orders message for exactly this bars
// message, in sequence, with this RunID, every order valid for the frame
// (Orders.Validate) and on an instrument of the run, and none at all on a warm-up
// frame. Anything else, a reply timeout, a decode failure or a disconnect ends the
// run: the worker is sent the error and the connection is closed, and the error
// is returned. ctx being done ends it as cancelled and returns ctx's error.
func (s *Session) Exchange(ctx context.Context, bars *proto.Bars) (*proto.Orders, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		s.endLocked("cancelled", func() { s.sendDone(proto.StatusCancelled, "run cancelled") })
		return nil, err
	}

	out := *bars
	out.Header = proto.Header{V: proto.Version, Type: proto.TypeBars, Seq: s.trSeq, RunID: s.runID}
	s.trSeq++
	_ = s.conn.SetWriteDeadline(time.Now().Add(s.cfg.ReplyTimeout))
	if err := proto.Write(s.conn, &out, s.cfg.MaxFrame); err != nil {
		var pe *proto.Error
		if errors.As(err, &pe) { // a bars message the codec itself refused: the host's bug
			s.failLocked(proto.CodeInternal, pe.Msg)
			return nil, &proto.Error{Code: proto.CodeInternal, Msg: pe.Msg}
		}
		s.dropLocked("write failed")
		return nil, fmt.Errorf("%w: %v", ErrDisconnected, err)
	}

	_ = s.conn.SetReadDeadline(time.Now().Add(s.cfg.ReplyTimeout))
	stop := context.AfterFunc(ctx, func() { _ = s.conn.SetReadDeadline(time.Now()) })
	m, err := proto.Read(s.conn, s.cfg.MaxFrame)
	stop()
	if err != nil {
		return nil, s.readFailedLocked(ctx, err)
	}
	return s.checkReplyLocked(m, &out)
}

func (s *Session) readFailedLocked(ctx context.Context, err error) error {
	var pe *proto.Error
	switch {
	case isTimeout(err) && ctx.Err() != nil:
		s.endLocked("cancelled", func() { s.sendDone(proto.StatusCancelled, "run cancelled") })
		return ctx.Err()
	case isTimeout(err):
		s.failLocked(proto.CodeTimeout, "no orders within "+s.cfg.ReplyTimeout.String())
		return &proto.Error{Code: proto.CodeTimeout, Msg: "no orders within " + s.cfg.ReplyTimeout.String()}
	case errors.As(err, &pe):
		s.failLocked(pe.Code, pe.Msg)
		return pe
	default: // end of stream or a transport failure: the worker is gone
		s.dropLocked("disconnected")
		return fmt.Errorf("%w: %v", ErrDisconnected, err)
	}
}

// checkReplyLocked applies ADR-094 sections 3 to 6 to the worker's message.
func (s *Session) checkReplyLocked(m proto.Message, sent *proto.Bars) (*proto.Orders, error) {
	fail := func(code proto.Code, format string, args ...any) (*proto.Orders, error) {
		msg := fmt.Sprintf(format, args...)
		s.failLocked(code, msg)
		return nil, &proto.Error{Code: code, Msg: msg}
	}
	switch m := m.(type) {
	case *proto.Failure:
		if h := m.Header; h.Seq != s.wkSeq {
			return fail(proto.CodeProtocol, "error has seq %d, want %d", h.Seq, s.wkSeq)
		} else if h.RunID != s.runID {
			return fail(proto.CodeProtocol, "error has run_id %q, want %q", h.RunID, s.runID)
		}
		s.dropLocked("worker error")
		return nil, &WorkerError{Failure: m}
	case *proto.Orders:
		h := m.Header
		switch {
		case h.Seq != s.wkSeq:
			return fail(proto.CodeProtocol, "orders has seq %d, want %d", h.Seq, s.wkSeq)
		case h.RunID != s.runID:
			return fail(proto.CodeProtocol, "orders has run_id %q, want %q", h.RunID, s.runID)
		case m.InReplyTo != sent.Seq:
			return fail(proto.CodeProtocol, "orders answers seq %d, but the bars sent was seq %d", m.InReplyTo, sent.Seq)
		case sent.Warm && len(m.Orders) > 0:
			return fail(proto.CodeInvalidOrder, "orders on a warm-up frame: a warm-up reply has none")
		}
		if err := m.Validate(sent.FrameAt); err != nil {
			return fail(proto.CodeOf(err), "%s", err.(*proto.Error).Msg)
		}
		for i, o := range m.Orders {
			if !s.instrSet[o.Instrument] {
				return fail(proto.CodeInvalidOrder, "orders[%d].instrument: %q is not an instrument of the run", i, o.Instrument)
			}
		}
		s.wkSeq++
		return m, nil
	default:
		return fail(proto.CodeProtocol, "expected orders, got %s", m.Head().Type)
	}
}

// Finish ends the run: it sends done with the status (completed, cancelled or
// failed) and the reason, then closes the connection after giving the worker a
// moment to close first. A session that has already ended returns ErrClosed.
func (s *Session) Finish(status, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	switch status {
	case proto.StatusCompleted, proto.StatusCancelled, proto.StatusFailed:
	default:
		return fmt.Errorf("server: %q is not a done status", status)
	}
	s.endLocked(status, func() { s.sendDone(status, reason) })
	return nil
}

// Fail ends the run with an error message to the worker and closes the connection.
// The host uses it for its own failures; the session uses it for the worker's.
func (s *Session) Fail(code proto.Code, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if !code.Valid() {
		code = proto.CodeInternal
	}
	s.failLocked(code, message)
	return nil
}

func (s *Session) failLocked(code proto.Code, message string) {
	s.endLocked("failed: "+string(code), func() {
		sendFailure(s.conn, s.cfg.MaxFrame, s.nextSeq(), s.runID, &proto.Error{Code: code, Msg: message})
	})
	s.log.Warn("run failed", "code", code, "reason", message)
}

// dropLocked closes a connection that is already gone or not worth talking to.
func (s *Session) dropLocked(why string) {
	s.endLocked("failed: "+why, func() {})
	s.log.Warn("run dropped", "reason", why)
}

// endLocked marks the run over, sends the farewell, and closes the connection.
func (s *Session) endLocked(outcome string, farewell func()) {
	if s.closed {
		return
	}
	s.closed = true
	s.outcome = outcome
	farewell()
	s.drainAndClose()
}

func (s *Session) nextSeq() uint64 {
	n := s.trSeq
	s.trSeq++
	return n
}

func (s *Session) sendDone(status, reason string) {
	_ = s.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_ = proto.Write(s.conn, &proto.Done{Header: proto.Header{Seq: s.nextSeq(), RunID: s.runID}, Status: status, Reason: reason}, s.cfg.MaxFrame)
}

// drainAndClose gives the worker a short grace to close its side first, then
// closes the connection.
func (s *Session) drainAndClose() {
	_ = s.conn.SetReadDeadline(time.Now().Add(s.cfg.CloseGrace))
	_, _ = io.Copy(io.Discard, io.LimitReader(s.conn, 64<<10))
	_ = s.conn.Close()
}

// sendFailure writes an error message, best effort.
func sendFailure(conn net.Conn, max int, seq uint64, runID string, err error) {
	code, msg := proto.CodeInternal, err.Error()
	var pe *proto.Error
	if errors.As(err, &pe) {
		code, msg = pe.Code, pe.Msg
	}
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_ = proto.Write(conn, &proto.Failure{Header: proto.Header{Seq: seq, RunID: runID}, Code: code, Fatal: true, Message: msg}, max)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
