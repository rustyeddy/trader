// Package server is the host side of ADR-094: it listens on loopback for
// workers, accepts one registration per connection as one run, and drives the
// lock-step exchange of bars and orders. It knows nothing about the engine, an
// account or a broker: the host supplies those decisions through Config (Admit
// decides whether and how a run is allowed, Handle runs it), so nothing a worker
// sends can choose an account, a size or a broker action.
//
// It imports only the standard library, marketdata, strategy and strategy/proto,
// and strategy/client must never import it, so a guest does not link host code.
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

// Defaults of ADR-094 section 7.
const (
	DefaultRegisterTimeout = 10 * time.Second
	DefaultReplyTimeout    = 30 * time.Second
)

// Grant is what the host decides about an admitted run. The worker asks; the
// host answers, and everything else about the run (the account, the risk policy,
// the data) stays with the host.
type Grant struct {
	// Instruments are the instruments of the run, in delivery order. Empty means
	// those the worker registered.
	Instruments []string
	// WarmBars is the warm-up the host will deliver (it may be fewer than asked).
	WarmBars int
}

// Admitter decides whether a registration may start a run: whether the
// instruments exist, whether the mode is supported, whether policy allows it. It
// returns Reject(...) to refuse the registration (the worker gets error rejected);
// any other error is the host's own failure (error internal). It runs after
// Register.Validate has passed.
type Admitter func(ctx context.Context, reg *proto.Register) (Grant, error)

// Handler runs one admitted run over the session. It is called after the worker
// has been told the RunID. A nil return finishes the run completed unless the
// handler finished it already; an error finishes it failed. It must return when
// ctx is done.
type Handler func(ctx context.Context, s *Session) error

// Config configures a Server. Admit and Handle are required.
type Config struct {
	Admit  Admitter
	Handle Handler

	// RegisterTimeout is how long a connection may take to send its register.
	// ReplyTimeout is how long a worker may take to answer one bars message. Zero
	// means the default of ADR-094.
	RegisterTimeout time.Duration
	ReplyTimeout    time.Duration
	// CloseGrace is how long a connection is kept open after the last message so
	// the worker can close first. Zero means a second.
	CloseGrace time.Duration
	// MaxFrame is the largest frame accepted. Zero means proto.DefaultMaxFrame.
	MaxFrame int
	// NewRunID issues a RunID, a ULID string. Zero means RandomRunID. It must not
	// repeat.
	NewRunID func() (string, error)
	// Logger receives the server's log lines. Nil discards them: a library does
	// not choose the process's logger.
	Logger *slog.Logger
}

// Server accepts workers.
type Server struct {
	cfg Config
	log *slog.Logger
}

// ErrRejected marks a registration the host refuses.
var ErrRejected = errors.New("registration rejected")

// Reject returns the error an Admitter gives to refuse a registration. The worker
// is sent the text.
func Reject(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRejected, fmt.Sprintf(format, args...))
}

// New returns a Server for cfg.
func New(cfg Config) (*Server, error) {
	if cfg.Admit == nil || cfg.Handle == nil {
		return nil, errors.New("server: Admit and Handle are required")
	}
	if cfg.RegisterTimeout <= 0 {
		cfg.RegisterTimeout = DefaultRegisterTimeout
	}
	if cfg.ReplyTimeout <= 0 {
		cfg.ReplyTimeout = DefaultReplyTimeout
	}
	if cfg.CloseGrace <= 0 {
		cfg.CloseGrace = time.Second
	}
	if cfg.MaxFrame <= 0 {
		cfg.MaxFrame = proto.DefaultMaxFrame
	}
	if cfg.NewRunID == nil {
		cfg.NewRunID = RandomRunID
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{cfg: cfg, log: log}, nil
}

// IsLoopbackAddr reports whether a listen address (host:port) binds to the
// loopback interface only. The host must be an IP literal or "localhost"; an empty
// host or 0.0.0.0 listens on every interface and is not loopback.
func IsLoopbackAddr(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, fmt.Errorf("listen address %q: %w", addr, err)
	}
	if host == "localhost" {
		return true, nil
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback(), nil
}

// Listen opens a TCP listener on addr, refusing an address that is not loopback
// (v1 has neither TLS nor a token, ADR-094 section 9).
func Listen(addr string) (net.Listener, error) {
	local, err := IsLoopbackAddr(addr)
	if err != nil {
		return nil, err
	}
	if !local {
		return nil, fmt.Errorf("listen address %q is not loopback: the strategy protocol has no TLS or token in v1", addr)
	}
	return net.Listen("tcp", addr)
}

// Serve accepts connections on ln until ctx is done or ln fails, then waits for
// the runs in flight (which are told to cancel). It refuses a listener that is not
// bound to loopback, whoever opened it. It closes ln.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	defer func() { _ = ln.Close() }()
	if tcp, ok := ln.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
		return fmt.Errorf("server: listener %s is not a loopback TCP address", ln.Addr())
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	var wg sync.WaitGroup
	// On every exit, tell the runs to cancel and close the listener before waiting
	// for them: a worker that never answers must not hold shutdown.
	defer func() {
		cancel()
		stop()
		_ = ln.Close()
		wg.Wait()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("server: accept: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.serveConn(ctx, conn)
		}()
	}
}

// serveConn is one connection's whole life: register, admission, the run.
func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	log := s.log.With("peer", conn.RemoteAddr().String())

	reg, err := s.readRegister(conn)
	if err != nil {
		if !errors.Is(err, errSilentClose) {
			s.sendError(conn, 1, "", err)
		}
		log.Info("registration refused", "err", err)
		return
	}
	grant, err := s.cfg.Admit(ctx, reg)
	if err != nil {
		s.sendError(conn, 1, "", admitError(err))
		log.Info("registration not admitted", "name", reg.Name, "err", err)
		return
	}
	if len(grant.Instruments) == 0 {
		grant.Instruments = reg.Run.Instruments
	}
	runID, err := s.cfg.NewRunID()
	if err != nil {
		s.sendError(conn, 1, "", &proto.Error{Code: proto.CodeInternal, Msg: "could not issue a run id"})
		log.Error("run id", "err", err)
		return
	}
	accepted := &proto.Accepted{Header: proto.Header{Seq: 1, RunID: runID}, Instruments: grant.Instruments,
		Interval: reg.Run.Interval, WarmBars: grant.WarmBars, ReplyTimeoutMS: int(s.cfg.ReplyTimeout / time.Millisecond)}
	_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.ReplyTimeout))
	if err := proto.Write(conn, accepted, s.cfg.MaxFrame); err != nil {
		log.Info("accepted not delivered", "run", runID, "err", err)
		return
	}

	sess := newSession(conn, s.cfg, runID, reg, grant, log.With("run", runID))
	log.Info("run accepted", "run", runID, "name", reg.Name, "build", reg.Build, "mode", reg.Run.Mode)
	s.run(ctx, sess)
}

func (s *Server) run(ctx context.Context, sess *Session) {
	err := s.cfg.Handle(ctx, sess)
	switch {
	case sess.isClosed():
	case ctx.Err() != nil:
		_ = sess.Finish(proto.StatusCancelled, "the server is shutting down")
	case err != nil:
		_ = sess.Finish(proto.StatusFailed, err.Error())
	default:
		_ = sess.Finish(proto.StatusCompleted, "")
	}
	sess.log.Info("run ended", "status", sess.status(), "err", err)
}

// errSilentClose marks a connection that ended before it said anything: there is
// no one to tell.
var errSilentClose = errors.New("connection closed before a register")

func (s *Server) readRegister(conn net.Conn) (*proto.Register, error) {
	_ = conn.SetReadDeadline(time.Now().Add(s.cfg.RegisterTimeout))
	m, err := proto.Read(conn, s.cfg.MaxFrame)
	if err != nil {
		switch {
		case errors.Is(err, io.EOF):
			return nil, errSilentClose
		case isTimeout(err):
			return nil, &proto.Error{Code: proto.CodeTimeout, Msg: "no register within " + s.cfg.RegisterTimeout.String()}
		}
		var pe *proto.Error
		if errors.As(err, &pe) {
			return nil, pe
		}
		return nil, errSilentClose // a transport failure: nobody to tell
	}
	reg, ok := m.(*proto.Register)
	if !ok {
		return nil, &proto.Error{Code: proto.CodeProtocol, Msg: fmt.Sprintf("expected register, got %s", m.Head().Type)}
	}
	if reg.Seq != 1 {
		return nil, &proto.Error{Code: proto.CodeProtocol, Msg: fmt.Sprintf("register has seq %d, want 1", reg.Seq)}
	}
	if err := reg.Validate(); err != nil {
		return nil, err
	}
	return reg, nil
}

func admitError(err error) *proto.Error {
	if errors.Is(err, ErrRejected) {
		return &proto.Error{Code: proto.CodeRejected, Msg: err.Error()}
	}
	return &proto.Error{Code: proto.CodeInternal, Msg: "the host could not admit the run"}
}

// sendError tells the peer why it is being dropped. It is best effort: the peer
// may already be gone.
func (s *Server) sendError(conn net.Conn, seq uint64, runID string, err error) {
	sendFailure(conn, s.cfg.MaxFrame, seq, runID, err)
}
