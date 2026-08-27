// Command trader is Trader's operator CLI, the first transport adapter
// over the application/service layer (ADR-022, issue #103; framework
// established by issue #108). It is a thin composition root: it wires
// configuration and logging, builds the command tree, and translates a
// command's returned error into a process exit code. It contains no
// business logic or use-case orchestration of its own — see
// docs/arch/adr-022-cli-app-service-layer.org for the boundary this
// package is built to respect.
//
// # Exit codes
//
// trader uses two exit codes: 0 on success, 1 for any error at all
// (an invalid flag, a cancelled context, a failed command). This is a
// deliberately minimal v0 convention — see
// docs/arch/adr-022-cli-app-service-layer.org's open questions for
// where a finer-grained convention (for example, distinguishing usage
// errors from execution failures) may be decided later, once real
// operator usage shows a need for it.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rustyeddy/trader/cmd/trader/internal/rootcmd"
)

func main() {
	os.Exit(run())
}

// run builds the command tree, executes it against a context that is
// cancelled on SIGINT/SIGTERM, and returns the process exit code. It is
// separated from main so tests can exercise the same wiring without
// calling os.Exit.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd, cleanup := rootcmd.New()
	defer func() { _ = cleanup() }()

	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "trader:", err)
		return 1
	}
	return 0
}
