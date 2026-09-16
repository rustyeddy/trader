package external

import (
	"context"
	"log/slog"
	"net"

	"google.golang.org/grpc"

	"github.com/rustyeddy/trader/logging"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
	"github.com/rustyeddy/trader/strategy"
)

// Host is the host-process side of Strategy Protocol v1
// (protocol/strategy/v1, ADR-062): a gRPC server implementing
// StrategyHostService. An external strategy process dials in as the
// gRPC client, Handshakes once, and opens the Run stream; Host
// translates that connection into an ordinary strategy.Strategy value
// (ExternalStrategyAdapter, issue #379) that a backtest.Runner or
// future live.Session can drive exactly like any in-process strategy
// — no special-case runner/scheduler logic is required.
//
// v1 assumes one strategy identity per connection (ADR-062's own
// scope statement); a Host serves one such connection's worth of
// strategy identity at a time (see Strategy's own doc comment).
type Host struct {
	logger *slog.Logger
	server *grpc.Server
	impl   *grpcServer
}

// NewHost returns a Host ready to Serve. A nil logger is accepted and
// treated as logging.Discard(), matching every other composition-root
// component's own convention.
func NewHost(logger *slog.Logger) *Host {
	if logger == nil {
		logger = logging.Discard()
	}
	impl := newGRPCServer(logger)
	server := grpc.NewServer()
	v1.RegisterStrategyHostServiceServer(server, impl)
	return &Host{logger: logger, server: server, impl: impl}
}

// Serve runs the gRPC server on lis until ctx is done or serving
// itself fails, whichever happens first — the explicit Run(ctx)
// lifecycle the architecture document requires of anything that owns
// background work (Trader starts no unmanaged goroutine): Serve owns
// lis and the server's own accept loop for its own duration, and does
// not return until both have actually stopped.
//
// Typical use starts Serve in its own goroutine, owned by the same ctx
// the composition root cancels to shut the whole run down:
//
//	go func() { _ = host.Serve(ctx, lis) }()
//	strat, err := host.Strategy(ctx)
//
// ctx cancellation calls the underlying grpc.Server's Stop, not
// GracefulStop: the Run RPC is a long-lived bidirectional stream that
// stays open for the entire session by design (strategy.proto's own
// Run doc comment — it ends only when the guest itself closes it, or
// on a transport error), so GracefulStop's own "wait for in-flight
// RPCs to finish naturally" contract would never return while a
// session is active. ctx cancellation is Trader's own explicit
// "end this run now" decision (the composition root's, not the
// guest's), so it tears the connection down immediately rather than
// waiting indefinitely for a remote peer that ADR-062 does not
// require to cooperate.
func (h *Host) Serve(ctx context.Context, lis net.Listener) error {
	errCh := make(chan error, 1)
	go func() { errCh <- h.server.Serve(lis) }()

	select {
	case <-ctx.Done():
		h.server.Stop()
		<-errCh
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// Strategy blocks until a guest completes Handshake, returning the
// resulting strategy.Strategy value (which additionally satisfies
// strategy.FillHandler and strategy.Closer when the guest negotiated
// CAPABILITY_FILL_HANDLER; Closer is always satisfied, see core.go).
// It reports ctx's own error if ctx is done first.
//
// Strategy must be called at most once per accepted connection: a
// second Handshake arriving before the first's strategy has been
// claimed here is rejected by the guest's own Handshake RPC
// (FailedPrecondition), not queued.
func (h *Host) Strategy(ctx context.Context) (strategy.Strategy, error) {
	select {
	case strat := <-h.impl.ready:
		return strat, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
