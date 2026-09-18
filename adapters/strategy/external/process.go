package external

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/strategy"
)

// SocketPathEnv is the environment variable Process sets on the child
// strategy process, naming the Unix-domain socket it must dial to
// reach this host's StrategyHostService (ADR-063). This is the entire
// discovery contract between Process and a guest: no command-line
// flag convention is assumed, since that would require Process to
// know how every possible guest language/runtime parses its own
// arguments, where an environment variable needs no such assumption.
const SocketPathEnv = "TRADER_STRATEGY_SOCKET"

// DefaultStartupTimeout is how long Launch waits for the child
// process to complete its own Handshake before giving up and
// reporting a startup failure (ADR-063). See LaunchConfig.StartupTimeout.
const DefaultStartupTimeout = 30 * time.Second

// DefaultShutdownGrace is how long Process.Stop waits after sending
// SIGTERM before escalating to SIGKILL (ADR-063). See
// LaunchConfig.ShutdownGrace.
const DefaultShutdownGrace = 5 * time.Second

// LaunchConfig configures Launch.
type LaunchConfig struct {
	// Command is the path to the strategy executable. Required.
	Command string
	// Args are passed to Command, unmodified.
	Args []string
	// Env is the child's own process environment, used verbatim
	// (together with SocketPathEnv, which Launch always appends) —
	// Launch never merges it with the host's own os.Environ(). Reading
	// the ambient process environment is a composition-root decision
	// (config/arch_test.go's own "no os.Getenv/Environ outside
	// config/cmd/test" rule), not one this adapter package makes on a
	// caller's behalf: a caller that wants the child to inherit the
	// host's environment passes os.Environ() itself as (part of) Env.
	Env []string
	// Dir is the child's working directory. Empty means the host
	// process's own working directory.
	Dir string

	// SocketPath pins the exact Unix-domain socket path to create,
	// resolved to an absolute path before use (a relative path would
	// otherwise resolve differently for this process, via net.Listen,
	// than for the child, which may run under a different Dir). If
	// empty, Launch generates one inside a fresh temporary directory
	// it owns and removes on Stop — the recommended default, since a
	// fixed, well-known path cannot support more than one concurrent
	// Process on one host and risks colliding with a stale file a
	// previous crashed run left behind (ADR-063's own "Alternatives
	// Considered"). A caller that sets SocketPath owns cleanup of the
	// path's own containing directory; Process only ever removes the
	// socket file itself in that case, never a caller-supplied
	// directory.
	//
	// A pre-existing path is cleared only when Launch can positively
	// establish it is a stale, unreachable Unix-domain socket — never
	// a regular file, and never a socket something is still actually
	// listening on; either of those fails Launch outright rather than
	// silently deleting or stealing it.
	SocketPath string

	// StartupTimeout overrides DefaultStartupTimeout. <= 0 uses the
	// default; there is no way to disable it entirely (unlike
	// HostOption's own callback/admission timeouts), since an
	// unbounded startup wait would defeat Launch's own purpose of
	// reporting a clear, timely failure.
	StartupTimeout time.Duration
	// ShutdownGrace overrides DefaultShutdownGrace.
	ShutdownGrace time.Duration

	// Logger receives structured lifecycle events and the child's own
	// captured stderr output. A nil Logger is treated as
	// logging.Discard().
	Logger *slog.Logger

	// HostOptions is forwarded to NewHost unchanged (callback/
	// admission timeouts for the resulting Host).
	HostOptions []HostOption
}

// Process is one launched external strategy child process, bound to
// the Unix-domain socket it dials into and the Host serving
// StrategyHostService on that socket (ADR-063). Construct one with
// Launch; a caller must call Stop exactly once, typically via
// t.Cleanup in tests or a defer in a composition root, to guarantee
// the child is reaped and the socket file is removed.
type Process struct {
	cmd    *exec.Cmd
	host   *Host
	lis    net.Listener
	strat  strategy.Strategy
	logger *slog.Logger

	sockPath      string
	ownedDir      string // non-empty only when Launch created this directory itself
	shutdownGrace time.Duration

	serveCtx    context.Context
	serveCancel context.CancelFunc
	serveDone   chan error // Host.Serve's own return

	stderrLog *lineLogger

	mu       sync.Mutex
	stopping bool
	waitDone chan struct{} // closed once cmd.Wait() returns
	waitErr  error         // valid once waitDone is closed

	cleanupOnce sync.Once
}

// Launch starts the configured strategy executable, opens the
// Unix-domain socket it must dial (naming the path via SocketPathEnv),
// and blocks until the guest completes Handshake, ctx is done, or
// cfg.StartupTimeout elapses — whichever happens first. A child that
// exits before completing Handshake fails Launch immediately with a
// clear error, rather than waiting out the remainder of the timeout
// (ADR-063).
//
// On success, the returned *Process's Strategy method returns the
// ready-to-drive strategy.Strategy value; the caller must call Stop
// exactly once when done. On failure, Launch has already terminated
// the child (if started) and cleaned up the socket before returning.
func Launch(ctx context.Context, cfg LaunchConfig) (*Process, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("external: launch: command must be set")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	startupTimeout := cfg.StartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = DefaultStartupTimeout
	}
	shutdownGrace := cfg.ShutdownGrace
	if shutdownGrace <= 0 {
		shutdownGrace = DefaultShutdownGrace
	}

	sockPath, ownedDir, err := resolveSocketPath(cfg.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("external: launch: %w", err)
	}

	lis, err := net.Listen("unix", sockPath)
	if err != nil {
		removeSocketDir(ownedDir, sockPath)
		return nil, fmt.Errorf("external: launch: listening on %s: %w", sockPath, err)
	}

	host := NewHost(logger, cfg.HostOptions...)
	serveCtx, serveCancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- host.Serve(serveCtx, lis) }()

	stderrLog := newLineLogger(logger, "external strategy stderr")

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = cfg.Dir
	cmd.Env = append(filterOutEnvKey(cfg.Env, SocketPathEnv), SocketPathEnv+"="+sockPath)
	cmd.Stderr = stderrLog

	p := &Process{
		host:          host,
		lis:           lis,
		logger:        logger,
		sockPath:      sockPath,
		ownedDir:      ownedDir,
		shutdownGrace: shutdownGrace,
		serveCtx:      serveCtx,
		serveCancel:   serveCancel,
		serveDone:     serveDone,
		stderrLog:     stderrLog,
		waitDone:      make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		p.cleanup()
		return nil, fmt.Errorf("external: launch: starting %s: %w", cfg.Command, err)
	}
	p.cmd = cmd
	logger.Info("external strategy process started", "command", cfg.Command, "pid", cmd.Process.Pid, "socket", sockPath)

	go p.reap()

	strat, err := p.awaitHandshake(ctx, startupTimeout)
	if err != nil {
		p.terminate(context.Background(), p.shutdownGrace)
		p.cleanup()
		return nil, err
	}
	p.strat = strat

	return p, nil
}

// awaitHandshake races the guest's own Handshake against ctx,
// startupTimeout, and the child exiting first.
func (p *Process) awaitHandshake(ctx context.Context, startupTimeout time.Duration) (strategy.Strategy, error) {
	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	type result struct {
		strat strategy.Strategy
		err   error
	}
	resultCh := make(chan result, 1)
	go func() {
		strat, err := p.host.Strategy(startupCtx)
		resultCh <- result{strat, err}
	}()

	select {
	case r := <-resultCh:
		if r.err != nil {
			return nil, fmt.Errorf("external: launch: waiting for handshake: %w", r.err)
		}
		return r.strat, nil
	case <-p.waitDone:
		cancel() // unblock host.Strategy so resultCh's goroutine doesn't leak
		<-resultCh
		exitErr := p.Err()
		if exitErr == nil {
			exitErr = fmt.Errorf("process exited cleanly (status 0) without ever completing handshake")
		}
		return nil, fmt.Errorf("external: launch: child process exited before completing handshake: %w", exitErr)
	}
}

// Strategy returns the strategy.Strategy value this Process resolved
// during Launch.
func (p *Process) Strategy() strategy.Strategy {
	return p.strat
}

// Done returns a channel closed when the child process has exited,
// for any reason — an unexpected crash or a Stop-initiated shutdown
// alike. See Err for the exit result once Done is closed.
//
// Process does not restart a crashed child or feed this signal into
// any runner policy itself (ADR-063): a composition root that wants
// "unexpected exit is a run failure" behavior selects on Done the same
// way it already reacts to an in-process strategy call returning an
// error or panicking.
func (p *Process) Done() <-chan struct{} {
	return p.waitDone
}

// Err returns the child process's own exit error. It is only valid
// once Done's channel is closed; before that it always reports nil.
// A clean (status 0) exit also reports nil — use Done to distinguish
// "still running" from "exited cleanly."
func (p *Process) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

// reap waits for the child to exit and records its result. If the
// exit was not caused by Stop, it proactively releases this Process's
// own resources (the Host's serve loop and the socket file) so a
// caller that never observed Done/Err before abandoning this Process
// does not leak them.
func (p *Process) reap() {
	err := p.cmd.Wait()

	p.mu.Lock()
	p.waitErr = err
	intentional := p.stopping
	p.mu.Unlock()
	close(p.waitDone)

	if err := p.stderrLog.Flush(); err != nil {
		p.logger.Warn("external strategy stderr: flushing trailing output", "error", err)
	}

	if !intentional {
		p.logger.Error("external strategy process exited unexpectedly", "error", err)
		p.cleanup()
	}
}

// Stop terminates the child process (SIGTERM, escalating to SIGKILL
// once ShutdownGrace elapses or ctx ends, whichever happens first),
// then always tears down the Host's serve loop and removes the socket
// file/directory — regardless of which signal actually ended the
// child. Stop is safe to call more than once and safe to call after
// the child has already exited on its own.
//
// ctx directly bounds the SIGTERM grace wait (not merely a snapshot
// of its deadline taken once): a ctx that is already done, or that
// ends before ShutdownGrace elapses, escalates to SIGKILL immediately
// rather than waiting out the full grace period regardless.
func (p *Process) Stop(ctx context.Context) error {
	p.mu.Lock()
	p.stopping = true
	p.mu.Unlock()

	p.terminate(ctx, p.shutdownGrace)
	p.cleanup()
	return nil
}

// terminate sends SIGTERM and waits for the child to exit, escalating
// to SIGKILL once grace elapses or ctx ends — whichever happens first
// — and is a no-op if the child was never started or has already
// exited.
func (p *Process) terminate(ctx context.Context, grace time.Duration) {
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}

	select {
	case <-p.waitDone:
		return // already exited
	default:
	}

	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		p.logger.Warn("external strategy process: sending SIGTERM", "error", err)
	}

	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case <-p.waitDone:
		return
	case <-ctx.Done():
		p.logger.Warn("external strategy process: caller context ended before the child exited, sending SIGKILL",
			"error", ctx.Err())
	case <-timer.C:
		p.logger.Warn("external strategy process: did not exit within shutdown grace period, sending SIGKILL",
			"grace", grace)
	}

	if err := p.cmd.Process.Kill(); err != nil {
		p.logger.Warn("external strategy process: sending SIGKILL", "error", err)
	}
	<-p.waitDone
}

// cleanup tears down the serve loop, closes the listener, and removes
// the socket file/directory. Idempotent.
func (p *Process) cleanup() {
	p.cleanupOnce.Do(func() {
		p.serveCancel()
		<-p.serveDone
		_ = p.lis.Close()
		removeSocketDir(p.ownedDir, p.sockPath)
	})
}

// resolveSocketPath returns the socket path Launch should listen on.
// An explicit path is resolved to an absolute path — cmd.Dir may
// differ from the host process's own working directory, so a relative
// path would resolve differently depending on which side (this
// process, via net.Listen, or the child, via SocketPathEnv) evaluates
// it (review finding) — and cleared of any stale socket left at that
// exact path by a previous crashed run (see clearStaleSocket). An
// empty path generates a fresh temporary directory, returned as
// ownedDir so cleanup removes it wholesale.
func resolveSocketPath(explicit string) (sockPath, ownedDir string, err error) {
	if explicit != "" {
		abs, absErr := filepath.Abs(explicit)
		if absErr != nil {
			return "", "", fmt.Errorf("resolving absolute socket path for %s: %w", explicit, absErr)
		}
		if err := clearStaleSocket(abs); err != nil {
			return "", "", err
		}
		return abs, "", nil
	}

	dir, err := os.MkdirTemp("", "trader-strategy-*")
	if err != nil {
		return "", "", fmt.Errorf("creating socket directory: %w", err)
	}
	return filepath.Join(dir, "strategy.sock"), dir, nil
}

// clearStaleSocket removes path only once it positively establishes
// that nothing is listening there — never a regular file, and never a
// live Unix-domain socket another process still owns (review finding:
// unconditionally removing whatever already sits at an explicit,
// caller-supplied path could delete an unrelated file, or unlink a
// live listener's own path out from under it while that listener is
// still reachable by its file descriptor, silently stealing the
// name). A path that does not exist at all is left alone — there is
// nothing to clear.
func clearStaleSocket(path string) error {
	info, statErr := os.Lstat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil
		}
		return fmt.Errorf("checking existing path %s: %w", path, statErr)
	}

	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("external: launch: %s already exists and is not a Unix-domain socket; refusing to remove it", path)
	}

	conn, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("external: launch: a process is already listening on %s", path)
	}

	// A failed dial is not, by itself, proof that nothing owns this
	// path (review finding): only the specific errors that positively
	// indicate an unbound, stale Unix-domain socket — ECONNREFUSED (no
	// listener behind the pathname), tolerating ENOENT (the path
	// vanished between the Lstat above and this dial, so there is
	// nothing left to remove) — authorize removal. Every other dial
	// failure (permission denied, a transient resource error, and so
	// on) fails closed, leaving the path untouched, rather than risk
	// unlinking a live socket this check simply could not verify.
	if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, syscall.ENOENT) {
		return fmt.Errorf("external: launch: %s exists but could not be verified as stale, refusing to remove it: %w", path, dialErr)
	}

	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		return fmt.Errorf("removing stale socket %s: %w", path, rmErr)
	}
	return nil
}

// filterOutEnvKey returns env with every entry naming key removed —
// used so Launch's own SocketPathEnv entry can never be shadowed or
// duplicated by a caller-supplied value of the same name (review
// finding: appending unconditionally could leave two entries for the
// same key on cmd.Env, whose effective value at runtime is undefined).
func filterOutEnvKey(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// removeSocketDir removes ownedDir (recursively) if set, otherwise
// just the socket file itself — mirroring resolveSocketPath's own
// ownership split. Errors are not fatal to callers (best-effort
// cleanup); this never panics or blocks.
func removeSocketDir(ownedDir, sockPath string) {
	if ownedDir != "" {
		_ = os.RemoveAll(ownedDir)
		return
	}
	if sockPath != "" {
		_ = os.Remove(sockPath)
	}
}

// lineLogger is an io.Writer that buffers partial writes and logs
// each complete line through logger, under msg — used to capture a
// child strategy process's own stderr as structured, actionable log
// records rather than discarding it or interleaving it unbuffered
// with the host's own output.
type lineLogger struct {
	logger *slog.Logger
	msg    string

	mu  sync.Mutex
	buf bytes.Buffer
}

func newLineLogger(logger *slog.Logger, msg string) *lineLogger {
	return &lineLogger{logger: logger, msg: msg}
}

func (w *lineLogger) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// No newline yet: put the partial line back and wait for more.
			w.buf.Reset()
			w.buf.WriteString(line)
			break
		}
		line = trimTrailingNewline(line)
		if line != "" {
			w.logger.Warn(w.msg, "line", line)
		}
	}
	return len(p), nil
}

// Flush logs any trailing output that never ended in a newline (for
// example a child that crashed mid-line).
func (w *lineLogger) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() == 0 {
		return nil
	}
	line := trimTrailingNewline(w.buf.String())
	w.buf.Reset()
	if line != "" {
		w.logger.Warn(w.msg, "line", line)
	}
	return nil
}

func trimTrailingNewline(s string) string {
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	return s
}
