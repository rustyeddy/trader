package strategy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

// requestTimeout (a variable so a test can shorten it) is how long Trader waits for a connected client's run request. A
// client that connects and says nothing must not hold the next one up.
var requestTimeout = 10 * time.Second

// Session is one accepted client, as Trader sees it. Client is its end of the
// connection: Trader sends the bars through it and reads the orders back. The
// connection belongs to ServeRuns, which closes it when the handler returns.
type Session struct {
	Client *Client
	// Remote is the client's address.
	Remote string

	conn     net.Conn
	accepted bool
}

// Accept tells the client its run exists, with the RunID. The handler calls it as
// soon as the run's journal does and before any bars are sent; a handler that
// returns an error without having called it refuses the run instead.
func (s *Session) Accept(runID string) error {
	if s.accepted {
		return errors.New("run already accepted")
	}
	if err := s.conn.SetWriteDeadline(time.Now().Add(requestTimeout)); err != nil {
		return err
	}
	if err := writeMessage(s.conn, startMessage{Accepted: &acceptedRun{RunID: runID}}); err != nil {
		return fmt.Errorf("sending accepted: %w", err)
	}
	s.accepted = true
	return nil
}

// RunHandler runs the backtest a client asked for. It is given the request, which
// has been validated, and the session; it calls Accept once the run exists and then
// trades through s.Client. What it returns ends the run: an error before Accept
// refuses it (the client gets the error text and no RunID), and once the run has
// been accepted the Result is sent as the last message, whatever the error.
type RunHandler func(ctx context.Context, req RunRequest, s *Session) (Result, error)

// ServeRuns accepts connections on ln, one at a time, until ctx ends or ln fails,
// and serves each as one run: read the run request, call handle, send the result,
// close. A second client waits in the listener's backlog while a run is active (a
// client bounds its own wait, see WithQueueTimeout). A client that fails, hangs up
// or sends a bad request ends only its own run; ServeRuns goes on to the next.
//
// ln must be a loopback listener. When ctx ends ServeRuns closes ln, lets the
// active run finish (the handler's ctx is the same, so it is cancelled) and returns
// nil.
func ServeRuns(ctx context.Context, ln net.Listener, handle RunHandler) error {
	if addr, ok := ln.Addr().(*net.TCPAddr); !ok || !addr.IP.IsLoopback() {
		return fmt.Errorf("listener %s is not loopback", ln.Addr())
	}
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		serveRun(ctx, conn, handle)
	}
}

// serveRun serves one connection and closes it.
func serveRun(ctx context.Context, conn net.Conn, handle RunHandler) {
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(requestTimeout))
	var in runMessage
	if err := readMessage(conn, &in); err != nil {
		refuse(conn, fmt.Sprintf("reading the run request: %v", err))
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	if err := in.Run.Validate(); err != nil {
		refuse(conn, "invalid run request: "+err.Error())
		return
	}

	s := &Session{Client: NewClient(conn), Remote: conn.RemoteAddr().String(), conn: conn}
	res, err := handle(ctx, in.Run, s)
	if !s.accepted {
		if err == nil {
			err = errors.New("the run was not started")
		}
		refuse(conn, err.Error())
		return
	}
	if err != nil && res.Error == "" {
		res.Error = err.Error()
	}
	_ = conn.SetWriteDeadline(time.Now().Add(requestTimeout))
	_ = writeMessage(conn, resultMessage{Result: res}) // the client may be the reason the run ended
}

// refuse tells the client why there is no run, if it is still listening.
func refuse(conn net.Conn, why string) {
	_ = conn.SetWriteDeadline(time.Now().Add(requestTimeout))
	_ = writeMessage(conn, startMessage{Error: why})
}

// options are what Run and its options set.
type options struct {
	warm         func(bars []marketdata.Bar)
	queueTimeout time.Duration
}

// Option changes how Run behaves.
type Option func(*options)

// WithWarmup gives the strategy the bars from before the range Trader trades, to
// prefill its indicators: it learns from them and decides nothing, so there is
// nothing to return. Without it those bars are acknowledged and ignored.
func WithWarmup(warm func(bars []marketdata.Bar)) Option {
	return func(o *options) { o.warm = warm }
}

// WithQueueTimeout bounds how long Run waits for Trader to accept the run. Trader
// serves one run at a time, so a client that arrives during another run waits;
// after d it gives up with an error instead of waiting forever. The default is
// DefaultTimeout.
func WithQueueTimeout(d time.Duration) Option {
	return func(o *options) { o.queueTimeout = d }
}
