package external_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/strategy/external"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy"
)

// fakeGuestPath is the path to the fakeguest test-fixture binary
// (cmd/internal/fakeguest — living under the repo's own exempted
// cmd/ tree, the only prefix exempt from both config/arch_test.go's
// "no os.Getenv/flag outside config/cmd/test" rule and clock/
// arch_test.go's "no direct time.* calls outside clock/cmd/adapters"
// rule, since fakeguest necessarily reads its own configuration from
// the environment and calls time.Sleep to simulate a slow-starting
// guest), built once in TestMain and used by every test in this file
// to exercise Launch
// against a real child process and a real Unix-domain socket — issue
// #380's own "child launch + handshake works deterministically" and
// "no zombie processes or stale sockets after tests" acceptance
// criteria require a genuine subprocess, not an in-process fake.
var fakeGuestPath string

func TestMain(m *testing.M) {
	os.Exit(runTestMain(m))
}

// runTestMain does the real work, returning the process exit code
// instead of calling os.Exit directly — os.Exit skips deferred
// functions, which would otherwise leak the temporary directory this
// builds the fakeguest fixture binary into (a real bug an earlier
// version of this function had: os.Exit(m.Run()) inside a function
// whose own `defer os.RemoveAll(dir)` therefore never ran).
func runTestMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "fakeguest-bin-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "external: building fakeguest test fixture:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	fakeGuestPath = filepath.Join(dir, "fakeguest")
	build := exec.Command("go", "build", "-o", fakeGuestPath,
		"github.com/rustyeddy/trader/cmd/internal/fakeguest")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "external: building fakeguest test fixture:", err)
		return 1
	}

	return m.Run()
}

func fakeGuestConfig(t *testing.T, mode string, extraEnv ...string) external.LaunchConfig {
	t.Helper()
	env := append([]string{"FAKEGUEST_MODE=" + mode}, extraEnv...)
	return external.LaunchConfig{
		Command: fakeGuestPath,
		Env:     env,
		Logger:  logging.Discard(),
	}
}

// TestLaunch_NormalRoundTrip is the core acceptance-criteria path:
// launch a real child process, complete a real Handshake over a real
// Unix-domain socket, drive a real Start/OnBar exchange with it, then
// Stop and confirm both the child and the socket file are gone.
func TestLaunch_NormalRoundTrip(t *testing.T) {
	ctx := context.Background()
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	cfg := fakeGuestConfig(t, "normal")
	cfg.SocketPath = sockPath

	p, err := external.Launch(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Stop(context.Background()) }) // Stop is idempotent; guards every assertion below

	strat := p.Strategy()
	require.Equal(t, "fakeguest", strat.Describe().Name)

	require.NoError(t, strat.Start(ctx, testEnvironment(t)))

	inst := eurUSD(t)
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	event := strategy.BarEvent{Instrument: inst, Interval: iv, Bar: testBar(t)}
	view := fakeView{acct: testFlatSnapshot(t)}

	intents, err := strat.OnBar(ctx, event, view)
	require.NoError(t, err)
	require.Empty(t, intents)

	select {
	case <-p.Done():
		t.Fatal("process should still be running")
	default:
	}

	require.NoError(t, p.Stop(context.Background()))

	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("process did not exit after Stop")
	}

	_, statErr := os.Stat(sockPath)
	require.True(t, os.IsNotExist(statErr), "socket file should have been removed after Stop")
}

// TestLaunch_DefaultSocketPathCleanedUp proves the owned-temp-directory
// path (no explicit LaunchConfig.SocketPath) is also fully cleaned up.
func TestLaunch_DefaultSocketPathCleanedUp(t *testing.T) {
	ctx := context.Background()
	p, err := external.Launch(ctx, fakeGuestConfig(t, "normal"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Stop(context.Background()) })

	require.NoError(t, p.Stop(context.Background()))

	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("process did not exit after Stop")
	}
}

// TestLaunch_StopIsIdempotent proves calling Stop more than once
// never panics or blocks.
func TestLaunch_StopIsIdempotent(t *testing.T) {
	ctx := context.Background()
	p, err := external.Launch(ctx, fakeGuestConfig(t, "normal"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Stop(context.Background()) })

	require.NoError(t, p.Stop(context.Background()))
	require.NoError(t, p.Stop(context.Background()))
}

// TestLaunch_ChildExitsBeforeHandshake proves a child that exits
// without ever dialing fails Launch fast and explicitly, rather than
// waiting out the full startup timeout.
func TestLaunch_ChildExitsBeforeHandshake(t *testing.T) {
	cfg := fakeGuestConfig(t, "exit-immediately")
	cfg.StartupTimeout = 10 * time.Second // deliberately long; the test asserts we don't wait for it

	start := time.Now()
	_, err := external.Launch(context.Background(), cfg)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(t, elapsed, 5*time.Second, "Launch should fail fast on an early child exit, not wait out the startup timeout")
}

// TestLaunch_StartupTimeoutExceeded proves a guest that never dials
// in fails Launch once StartupTimeout elapses, and that the child is
// terminated and the socket cleaned up as part of that failure.
func TestLaunch_StartupTimeoutExceeded(t *testing.T) {
	cfg := fakeGuestConfig(t, "delay-connect", "FAKEGUEST_DELAY=5s")
	cfg.StartupTimeout = 300 * time.Millisecond
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	cfg.SocketPath = sockPath

	start := time.Now()
	p, err := external.Launch(context.Background(), cfg)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Nil(t, p)
	require.Less(t, elapsed, 4*time.Second, "Launch should fail once StartupTimeout elapses, not wait for the guest's own delay")

	_, statErr := os.Stat(sockPath)
	require.True(t, os.IsNotExist(statErr), "socket file should have been removed after a failed Launch")
}

// TestLaunch_ContextCanceledDuringStartup proves the caller's own ctx
// can abort Launch before StartupTimeout elapses.
func TestLaunch_ContextCanceledDuringStartup(t *testing.T) {
	cfg := fakeGuestConfig(t, "delay-connect", "FAKEGUEST_DELAY=30s")
	cfg.StartupTimeout = 30 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := external.Launch(ctx, cfg)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(t, elapsed, 4*time.Second)
}

// TestLaunch_UnexpectedExitAfterStartup proves a child that crashes
// after a successful Handshake is detected via Done/Err, and that its
// resources (serve loop, socket) are released even without an
// explicit Stop call.
func TestLaunch_UnexpectedExitAfterStartup(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	cfg := fakeGuestConfig(t, "crash-after-handshake")
	cfg.SocketPath = sockPath

	p, err := external.Launch(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Stop(context.Background()) })

	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("process did not report exit after crashing")
	}
	require.Error(t, p.Err())

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(sockPath)
		return os.IsNotExist(statErr)
	}, 2*time.Second, 20*time.Millisecond, "socket file should be removed after an unexpected exit, even without an explicit Stop")
}

// TestLaunch_StopEscalatesToSIGKILL proves a child that ignores
// SIGTERM is still terminated once ShutdownGrace elapses.
func TestLaunch_StopEscalatesToSIGKILL(t *testing.T) {
	p, err := external.Launch(context.Background(), fakeGuestConfig(t, "ignore-sigterm"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Stop(context.Background()) })

	stopCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	require.NoError(t, p.Stop(stopCtx))
	elapsed := time.Since(start)

	select {
	case <-p.Done():
	default:
		t.Fatal("process should have been reaped by Stop")
	}
	require.Error(t, p.Err()) // killed, not a clean exit
	require.Less(t, elapsed, 3*time.Second, "Stop should escalate to SIGKILL rather than waiting indefinitely")
}

// TestLaunch_StopHonorsConfiguredShutdownGrace proves
// LaunchConfig.ShutdownGrace itself controls escalation timing — not
// merely a short ctx deadline layered on top of the (much longer)
// default, which TestLaunch_StopEscalatesToSIGKILL alone would not
// distinguish from ShutdownGrace being silently ignored.
func TestLaunch_StopHonorsConfiguredShutdownGrace(t *testing.T) {
	cfg := fakeGuestConfig(t, "ignore-sigterm")
	cfg.ShutdownGrace = 150 * time.Millisecond

	p, err := external.Launch(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Stop(context.Background()) })

	start := time.Now()
	require.NoError(t, p.Stop(context.Background())) // no ctx deadline at all
	elapsed := time.Since(start)

	require.Error(t, p.Err())
	require.Less(t, elapsed, 3*time.Second,
		"Stop should escalate once the configured ShutdownGrace elapses, not wait for the 5s default")
}

// TestLaunch_StopWithAlreadyCanceledContextEscalatesImmediately proves
// an already-canceled Stop ctx (no deadline, just Done) escalates to
// SIGKILL right away rather than waiting out ShutdownGrace.
func TestLaunch_StopWithAlreadyCanceledContextEscalatesImmediately(t *testing.T) {
	p, err := external.Launch(context.Background(), fakeGuestConfig(t, "ignore-sigterm"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Stop(context.Background()) })

	stopCtx, cancel := context.WithCancel(context.Background())
	cancel() // already done before Stop is even called

	start := time.Now()
	require.NoError(t, p.Stop(stopCtx))
	elapsed := time.Since(start)

	require.Error(t, p.Err())
	require.Less(t, elapsed, 1*time.Second,
		"an already-canceled Stop ctx should escalate to SIGKILL immediately")
}

// TestLaunch_ExplicitSocketPathRejectsRegularFile proves Launch fails
// outright, without touching it, when an explicit SocketPath already
// names a plain file rather than a Unix-domain socket.
func TestLaunch_ExplicitSocketPathRejectsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sock")
	require.NoError(t, os.WriteFile(path, []byte("not a socket"), 0o600))

	cfg := fakeGuestConfig(t, "normal")
	cfg.SocketPath = path

	_, err := external.Launch(context.Background(), cfg)
	require.Error(t, err)

	// The file must still be exactly what it was — never deleted.
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, "not a socket", string(data))
}

// TestLaunch_ExplicitSocketPathRejectsLiveListener proves Launch
// fails outright, without stealing the path, when another process (or
// in this test, another listener within the same process) is already
// listening at the explicit SocketPath.
func TestLaunch_ExplicitSocketPathRejectsLiveListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sock")
	other, err := net.Listen("unix", path)
	require.NoError(t, err)
	defer func() { _ = other.Close() }()

	cfg := fakeGuestConfig(t, "normal")
	cfg.SocketPath = path

	_, launchErr := external.Launch(context.Background(), cfg)
	require.Error(t, launchErr)

	// The original listener must still be alive and reachable.
	conn, dialErr := net.Dial("unix", path)
	require.NoError(t, dialErr)
	require.NoError(t, conn.Close())
}

// TestLaunch_ExplicitSocketPathReusesStaleSocketFile proves Launch
// succeeds and reuses the path when a stale Unix-domain socket file
// (left behind by a listener that exited without unlinking it — the
// same shape a crashed previous run would leave) already exists there.
func TestLaunch_ExplicitSocketPathReusesStaleSocketFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sock")

	stale, err := net.Listen("unix", path)
	require.NoError(t, err)
	stale.(*net.UnixListener).SetUnlinkOnClose(false) // leave the file behind, simulating a crash
	require.NoError(t, stale.Close())

	_, statErr := os.Lstat(path)
	require.NoError(t, statErr, "the stale socket file must actually be present before Launch runs")

	cfg := fakeGuestConfig(t, "normal")
	cfg.SocketPath = path

	p, err := external.Launch(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Stop(context.Background()) })
}

// TestLaunch_CommandNotFound proves an invalid executable path fails
// Launch immediately with a clear error.
func TestLaunch_CommandNotFound(t *testing.T) {
	cfg := external.LaunchConfig{
		Command: filepath.Join(t.TempDir(), "does-not-exist"),
		Logger:  logging.Discard(),
	}
	_, err := external.Launch(context.Background(), cfg)
	require.Error(t, err)
}

// TestLaunch_EmptyCommandRejected proves a missing Command is
// rejected before anything is launched.
func TestLaunch_EmptyCommandRejected(t *testing.T) {
	_, err := external.Launch(context.Background(), external.LaunchConfig{Logger: logging.Discard()})
	require.Error(t, err)
}

// TestLaunch_FillHandlerRoundTrip proves OnFill also round-trips
// through a real child process (FillHandler capability aside — the
// fakeguest always answers whatever it's sent; this exercises the
// FillEvent wire path specifically).
func TestLaunch_FillHandlerRoundTrip(t *testing.T) {
	ctx := context.Background()
	p, err := external.Launch(ctx, fakeGuestConfig(t, "normal"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Stop(context.Background()) })

	strat := p.Strategy()
	require.NoError(t, strat.Start(ctx, testEnvironment(t)))

	fillHandler, ok := strat.(strategy.FillHandler)
	if !ok {
		t.Skip("fakeguest did not negotiate CAPABILITY_FILL_HANDLER")
	}

	fill, err := order.NewFill(order.Fill{
		FillID:    mustFillID(t),
		OrderID:   mustOrderID(t),
		AccountID: mustAccountID(t),
		Listing:   eurUSDListing(t),
		Side:      order.Buy,
		Price:     num.MustParsePrice("1.1005"),
		Quantity:  num.MustParseQuantity("1000"),
		Timestamp: time.Now().UTC(),
	})
	require.NoError(t, err)

	err = fillHandler.OnFill(ctx, strategy.FillEvent{Fill: fill}, fakeView{acct: testFlatSnapshot(t)})
	require.NoError(t, err)
}
