package backtestcfg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rustyeddy/trader/internal/adapters/strategy/external"
	svcbacktest "github.com/rustyeddy/trader/internal/service/backtest"
)

// StrategyConfigPathEnv names the environment variable a run sets on an
// external strategy child process (alongside external.SocketPathEnv,
// which Launch always sets) when ExternalStrategy.Config is given. This
// is a Trader-owned convention, not part of Strategy Protocol v1 itself
// (ADR-062 defines no config-handoff mechanism — a guest's own
// configuration schema is entirely its author's business); it exists
// only so an author who does want a config file does not have to invent
// their own argv convention. Named the same way external.SocketPathEnv
// is (an environment variable, not a CLI flag) for the identical reason
// that constant's own doc comment gives: no command-line
// argument-parsing convention can be assumed across every possible
// guest language/runtime.
const StrategyConfigPathEnv = "TRADER_STRATEGY_CONFIG"

// strategyConfigPathEnv is StrategyConfigPathEnv, for this package's
// existing references.
const strategyConfigPathEnv = StrategyConfigPathEnv

// ExternalStrategy launches an out-of-tree strategy executable over
// Strategy Protocol v1 (ADR-062/ADR-063).
type ExternalStrategy struct {
	// Exec is the executable: a path, or a bare name looked up on PATH.
	Exec string
	// Args are passed to the executable unmodified.
	Args []string
	// Config, if set, is a config-file path forwarded to the child as
	// StrategyConfigPathEnv, never parsed by Trader itself.
	Config string
	// Environ is the environment the child inherits.
	Environ []string
}

// externalStrategyParams is the report.BacktestReport-visible record
// of how --strategy-exec launched the external strategy (never its
// own runtime StrategyDescriptor — that already becomes the
// manifest's own StrategyName via strat.Describe(), the same as any
// in-tree strategy). This is deliberately the only strategy-specific
// state this command records for an external run: the config file
// itself, if any, is opaque to trader (see strategyConfigPathEnv's own
// doc comment), so there is nothing further here that could be
// meaningfully validated or replayed.
type externalStrategyParams struct {
	// Mode is always "external" — a fixed, explicit marker
	// distinguishing this run's own manifest provenance shape from
	// the in-tree EMA/demo paths' own StrategyParameters shapes at a
	// glance (issue #385).
	Mode string `json:"mode"`
	// StrategyName/StrategyVersion are the external strategy's own
	// Descriptor (ADR-062), received during Handshake — the actual
	// strategy identity that ran, independent of --strategy-exec's
	// own path/name. Manifest.StrategyName() also already carries
	// StrategyName (via strat.Describe(), the same as any in-tree
	// strategy); it is repeated here so it travels with the rest of
	// this run's own external-specific provenance in one place.
	StrategyName    string `json:"strategy_name"`
	StrategyVersion string `json:"strategy_version"`
	// ProtocolVersion is the Strategy Protocol version this run
	// actually negotiated (ADR-062) — recorded so a manifest from a
	// future, incompatible protocol revision is distinguishable at a
	// glance, without cross-referencing the trader binary's own build
	// version.
	ProtocolVersion string `json:"protocol_version"`
	// Transport is always "unix" for Strategy Protocol v1 (ADR-063) —
	// the endpoint/transport kind issue #385 asks be recorded. No
	// other transport exists yet, but naming it explicitly avoids a
	// silent assumption once one does. Never the ephemeral Unix-domain
	// socket *path* itself: Launch generates a fresh one per run
	// (external.LaunchConfig.SocketPath's own doc comment), and issue
	// #385 explicitly excludes any such machine-specific, ephemeral
	// value from a run's semantic identity.
	Transport string `json:"transport"`
	// Exec is the resolved absolute path to the launched executable —
	// never a relative spelling that would resolve differently from a
	// different working directory (the same reason Config, below, is
	// already resolved to an absolute path).
	Exec string `json:"exec"`
	// ExecDigest is a "sha256:<hex>" content digest of the executable
	// at Exec, computed at launch time — matching backtest.Manifest.
	// ConfigDigest's own convention. Exec's path/name alone cannot
	// distinguish two different builds behind the identical
	// --strategy-exec path (issue #385's own explicit acceptance
	// criterion: "executable identity/digest is stable enough to
	// distinguish different builds").
	ExecDigest string   `json:"exec_digest"`
	Args       []string `json:"args,omitempty"`
	Config     string   `json:"config,omitempty"`
	// ConfigDigest is a "sha256:<hex>" content digest of the exact
	// bytes at Config, computed before launch (review finding: Config
	// alone records only a *path*, not the bytes at that path — a
	// config file edited in place between two runs using the identical
	// path would otherwise let both runs record the same provenance
	// despite the guest actually consuming different configuration,
	// defeating this issue's own reproducibility goal). Empty exactly
	// when Config is empty (no --strategy-config given).
	ConfigDigest string `json:"config_digest,omitempty"`
}

// fileContentDigest returns a deterministic "sha256:<hex>" content
// digest of the file at path — streamed via io.Copy into the hash
// rather than os.ReadFile (review finding: provenance hashing has no
// need to allocate an executable's entire content into memory at
// once) — matching backtest.Manifest.ConfigDigest's own "sha256:<hex>"
// convention. Shared by both the executable and the strategy config's
// own digest.
// ctx bounds this operation per the architecture document's own
// "Use context.Context on operations that may block, perform I/O, or
// span a use case" convention, matching every other I/O call in this
// file (src.load, nextBarOpenAfterEntry). Checked once, before
// opening the file: a canceled/expired ctx fails fast rather than
// still reading and hashing a potentially large executable no caller
// will use the result of. The read itself is local disk I/O expected
// to complete quickly, so io.Copy is not further split into a
// ctx-selecting loop mid-stream (review finding raised the missing
// parameter; this is the proportionate response for a bounded local
// file read, not network I/O).
func fileContentDigest(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("computing content digest: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("computing content digest: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// resolveStrategyExecutable resolves flags.strategyExec to the exact
// absolute path external.Launch will actually execute (review
// finding): filepath.Abs(name) alone is not equivalent to what
// os/exec does with a bare command name containing no path
// separator, which searches PATH (exec.LookPath) rather than
// resolving relative to the current directory — using the wrong one
// could hash and record a different file than the one that actually
// ran. A name that does contain a path separator is never looked up
// on PATH by os/exec either way, so it is resolved with plain
// filepath.Abs, matching os/exec's own literal-path handling exactly.
func resolveStrategyExecutable(name string) (string, error) {
	resolved := name
	if !strings.ContainsRune(name, os.PathSeparator) {
		p, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("resolving --strategy-exec: %w", err)
		}
		resolved = p
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolving --strategy-exec: %w", err)
	}
	return abs, nil
}

// buildExternalLaunchConfig assembles the external.LaunchConfig
// --strategy-exec launches with (LaunchConfig.Command set to the
// exact resolved executable path resolveStrategyExecutable returns —
// review finding: an earlier version passed flags.strategyExec
// verbatim, which could resolve to a different file than whatever
// this function's own caller separately hashed for provenance), and
// the resolved absolute --strategy-config path (empty if none was
// given) to record in externalStrategyParams. Pulled out as its own
// pure function, rather than inlined in runBacktest's own strategy-
// selection branch, so its env-inheritance and path-resolution
// behavior can be unit tested directly against a synthetic environ,
// without spawning a real process (review finding: an earlier version
// of this command left Env nil/near-empty instead of starting from
// the operator's own inherited environment — see filterOutEnvKey's
// own doc comment for why environ is filtered before
// strategyConfigPathEnv is appended).
func buildExternalLaunchConfig(ext ExternalStrategy, logger *slog.Logger) (cfg external.LaunchConfig, resolvedExec, configAbs string, err error) {
	resolvedExec, err = resolveStrategyExecutable(ext.Exec)
	if err != nil {
		return external.LaunchConfig{}, "", "", err
	}
	cfg = external.LaunchConfig{
		Command: resolvedExec,
		Args:    ext.Args,
		Env:     filterOutEnvKey(ext.Environ, strategyConfigPathEnv),
		Logger:  logger,
	}
	if ext.Config == "" {
		return cfg, resolvedExec, "", nil
	}
	abs, err := filepath.Abs(ext.Config)
	if err != nil {
		return external.LaunchConfig{}, "", "", fmt.Errorf("resolving --strategy-config: %w", err)
	}
	cfg.Env = append(cfg.Env, strategyConfigPathEnv+"="+abs)
	return cfg, resolvedExec, abs, nil
}

// filterOutEnvKey returns env with every entry naming key removed —
// used so a caller-controlled Env value (os.Environ(), here) can never
// end up with two entries for the same key once this command's own
// value is appended, whose effective value at runtime is undefined;
// mirrors external.Launch's own identical filter for its socket
// variable (adapters/strategy/external/process.go).
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

// processMonitor is the *external.Process surface
// runWithExternalProcessMonitor needs — narrowed to an interface so a
// deterministic fake can exercise the exact simultaneous-readiness
// race below without timing luck (review finding).
type processMonitor interface {
	Done() <-chan struct{}
	Err() error
}

// runWithExternalProcessMonitor calls svc.Run, concurrently watching
// process.Done() (a nil process — every non-external-strategy path —
// disables this and simply calls svc.Run directly). If the external
// process exits before svc.Run itself returns, the run's own context
// is canceled and the process's exit is reported as the failure
// reason: a guest that crashes (or exits cleanly but unexpectedly)
// between callbacks, or during a replay that never calls back into it
// at all, must not let an unaffected Scheduler finish "successfully"
// with no idea the strategy driving it is already gone (review
// finding).
//
// A successful resultCh receive is not, by itself, proof the guest
// was still alive when the run finished: Go's select chooses
// pseudo-randomly among simultaneously ready cases, so a process that
// exits at (or just before) the exact instant svc.Run completes can
// still be the resultCh arm selected, silently accepting success from
// a guest that already crashed (second-round review finding — this is
// exactly the failure class this function exists to close). A nil-err
// result is therefore always followed by one more non-blocking check
// of process.Done() before it is accepted.
func runWithExternalProcessMonitor(ctx context.Context, svc *svcbacktest.Service, req svcbacktest.RunRequest, process processMonitor) (svcbacktest.RunResponse, error) {
	if process == nil {
		return svc.Run(ctx, req)
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	resultCh := make(chan runResult, 1)
	go func() {
		resp, err := svc.Run(runCtx, req)
		resultCh <- runResult{resp, err}
	}()

	return awaitRunWithProcessMonitor(cancelRun, resultCh, process)
}

// runResult is svc.Run's own return pair, carried over resultCh.
type runResult struct {
	resp svcbacktest.RunResponse
	err  error
}

// awaitRunWithProcessMonitor is runWithExternalProcessMonitor's own
// select/race-handling logic, extracted so it can be exercised
// directly against a synthetic resultCh and a fake processMonitor —
// runWithExternalProcessMonitor itself needs a real *svcbacktest.
// Service, too heavy to construct just to prove this race is closed
// (review finding: a deterministic test, not timing luck).
func awaitRunWithProcessMonitor(cancelRun context.CancelFunc, resultCh <-chan runResult, process processMonitor) (svcbacktest.RunResponse, error) {
	select {
	case r := <-resultCh:
		if r.err != nil {
			return r.resp, r.err
		}
		return r.resp, checkProcessStillAlive(process)
	case <-process.Done():
		cancelRun()
		r := <-resultCh
		if procErr := process.Err(); procErr != nil {
			return r.resp, fmt.Errorf("external strategy process exited unexpectedly before the run completed: %w", procErr)
		}
		if r.err == nil {
			return r.resp, fmt.Errorf("external strategy process exited before the run completed")
		}
		return r.resp, r.err
	}
}

// checkProcessStillAlive reports the process's own exit as an error
// if it has already exited by the time this is called (a non-blocking
// check: Done() open means "still running," not "will never exit"),
// nil otherwise. Called only after svc.Run itself already reported
// success — see runWithExternalProcessMonitor's own doc comment for
// why this second check exists.
func checkProcessStillAlive(process processMonitor) error {
	select {
	case <-process.Done():
		if err := process.Err(); err != nil {
			return fmt.Errorf("external strategy process exited unexpectedly before the run completed: %w", err)
		}
		return fmt.Errorf("external strategy process exited before the run completed")
	default:
		return nil
	}
}
