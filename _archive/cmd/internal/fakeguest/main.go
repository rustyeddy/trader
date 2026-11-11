// Command fakeguest is a minimal Strategy Protocol v1 guest, used only
// as a real out-of-process test fixture for
// adapters/strategy/external's own process_test.go (issue #380). It
// is not part of any published SDK — a real guest is sdk,
// issue #381 — and exists solely to exercise Process/Launch against
// an actual child process and Unix-domain socket rather than an
// in-process fake, which is exactly what issue #380's own acceptance
// criteria ("child launch + handshake works deterministically", "no
// zombie processes or stale sockets after tests") require proving
// against.
//
// Behavior is selected by the FAKEGUEST_MODE environment variable:
//
//   - "normal" (default): dial the socket named by
//     external.SocketPathEnv, Handshake, open Run, and answer every
//     BarEvent with an empty OnBarResponse (no intents/signals) until
//     the stream ends or SIGTERM is received, then exit 0.
//   - "delay-connect": sleep FAKEGUEST_DELAY (a time.Duration string,
//     default 5s) before dialing at all.
//   - "exit-immediately": exit 0 without ever dialing.
//   - "crash-after-handshake": Handshake and open Run normally, then
//     os.Exit(1) immediately without answering any BarEvent.
//   - "crash-after-first-bar": like "normal", except this mode alone
//     declares one EUR/USD H1 DataRequirement at Handshake (every
//     other mode leaves Requirements empty), answers exactly the
//     first BarEvent, then os.Exit(1)s immediately — a real crash
//     between two Scheduler callbacks, for cmd/trader/backtest's own
//     "an external process exit mid-run is reported as a run failure"
//     regression (issue #382 review), which a zero-Requirements crash
//     cannot exercise since backtest.Runner already rejects an empty
//     universe outright before Scheduler ever runs.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func main() {
	if err := run(); err != nil {
		log.Printf("fakeguest: %v", err)
		os.Exit(1)
	}
}

func run() error {
	mode := os.Getenv("FAKEGUEST_MODE")
	if mode == "" {
		mode = "normal"
	}

	if mode == "exit-immediately" {
		return nil
	}

	if mode == "delay-connect" {
		delay := 5 * time.Second
		if s := os.Getenv("FAKEGUEST_DELAY"); s != "" {
			d, err := time.ParseDuration(s)
			if err != nil {
				return fmt.Errorf("parsing FAKEGUEST_DELAY: %w", err)
			}
			delay = d
		}
		time.Sleep(delay)
	}

	sockPath := os.Getenv("TRADER_STRATEGY_SOCKET")
	if sockPath == "" {
		return fmt.Errorf("TRADER_STRATEGY_SOCKET not set")
	}

	conn, err := grpc.NewClient("unix:"+sockPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dialing %s: %w", sockPath, err)
	}
	defer func() { _ = conn.Close() }()

	client := v1.NewStrategyHostServiceClient(conn)
	ctx := context.Background()

	descriptor := &v1.StrategyDescriptor{
		Name:    "fakeguest",
		Version: "0.0.0",
	}
	if mode == "crash-after-first-bar" {
		// Only this mode declares a requirement — every other mode's
		// Requirements stays empty, unchanged, so existing tests
		// against this fixture keep seeing exactly the behavior they
		// already assert. This one requirement gives Scheduler a real,
		// single bar to deliver before this process exits mid-run,
		// exercising the "process exits between callbacks" case a
		// zero-Requirements crash cannot (backtest.Runner already
		// rejects an empty universe outright, before Scheduler ever
		// runs).
		descriptor.Requirements = []*v1.DataRequirement{{
			InstrumentId: "fx:EUR/USD",
			Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1},
		}}
	}

	hsResp, err := client.Handshake(ctx, &v1.HandshakeRequest{
		ProtocolVersion:    v1.ProtocolVersion,
		StrategyDescriptor: descriptor,
		Capabilities:       []v1.Capability{v1.Capability_CAPABILITY_FILL_HANDLER},
	})
	if err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	if !hsResp.GetAccepted() {
		return fmt.Errorf("handshake rejected: %v", hsResp.GetRejectReason())
	}

	stream, err := client.Run(ctx)
	if err != nil {
		return fmt.Errorf("opening run stream: %w", err)
	}
	if err := stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_RunOpen{
		RunOpen: &v1.RunOpen{SessionId: hsResp.GetSessionId()},
	}}); err != nil {
		return fmt.Errorf("sending run_open: %w", err)
	}

	if mode == "crash-after-handshake" {
		os.Exit(1)
	}

	if mode == "ignore-sigterm" {
		// SIGTERM's default disposition is to terminate the process,
		// so simply not calling signal.Notify would not actually test
		// anything — explicitly ignore it, so this process only ever
		// exits on SIGKILL. A test fixture for Process.Stop's own
		// SIGTERM-then-SIGKILL escalation.
		signal.Ignore(syscall.SIGTERM)
	} else {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() {
			<-sigCh
			os.Exit(0)
		}()
	}

	for {
		msg, err := stream.Recv()
		if err != nil {
			return nil // host ended the stream; exit cleanly
		}

		switch payload := msg.GetPayload().(type) {
		case *v1.RunServerMessage_SessionStart:
			// Nothing to do; wait for the first bar event.
		case *v1.RunServerMessage_BarEvent:
			if err := stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_OnBarResponse{
				OnBarResponse: &v1.OnBarResponse{Sequence: payload.BarEvent.GetSequence()},
			}}); err != nil {
				return fmt.Errorf("responding to bar_event: %w", err)
			}
			if mode == "crash-after-first-bar" {
				os.Exit(1)
			}
		case *v1.RunServerMessage_FillEvent:
			if err := stream.Send(&v1.RunClientMessage{Payload: &v1.RunClientMessage_OnFillResponse{
				OnFillResponse: &v1.OnFillResponse{Sequence: payload.FillEvent.GetSequence()},
			}}); err != nil {
				return fmt.Errorf("responding to fill_event: %w", err)
			}
		case *v1.RunServerMessage_SessionEnd:
			return nil
		}
	}
}
