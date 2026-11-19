package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/service"
)

type migrateFlags struct {
	config, dataRoot string
	apply, reverse   bool
	checksum         bool
}

func newMigrateCmd(a *app) *cobra.Command {
	var f migrateFlags
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Move the raw archive to the current layout",
		Long: `Move the raw archive from the old layout to the one trader reads:

  <data root>/raw/<source>/...   becomes   <data root>/<source>/raw/...

Each source's directory is renamed as a whole. Nothing is copied: on one filesystem a
rename is atomic and takes no longer for a million files than for one, and every
file keeps its inode and its bytes. Only raw is moved. The old canonical/ files are
canonical-v1: they are never moved or touched, only listed, and are rebuilt with
trader data build; delete them yourself once a rebuild passes trader data audit.

It is a DRY RUN unless you pass --apply: it plans, checks and prints, and changes
nothing. It refuses, and moves nothing at all, if a destination already exists, if a
source holds a symbolic link, or if it finds a temporary file (a download or build is
running or was interrupted: stop it first). After each move it verifies that the files
are the files that were there (same set, sizes, modification times and inodes);
--checksum also compares the SHA-256 of every file before and after (it reads all the
data twice). A failed verification is reported and nothing is moved back for you:
--reverse moves everything back, with the same checks. Run it only when no download
or build is running. The data root is --data-root, then ` + envDataRoot + `, then marketdata.root in
` + defaultConfigPath + `, then the default ` + store.DefaultRoot + `.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runMigrate(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.config, "config", defaultConfigPath, "config file (a missing default file is fine when flags or the environment supply the values)")
	fl.StringVar(&f.dataRoot, "data-root", "", "data root (default: "+envDataRoot+", then marketdata.root in the config file, then "+store.DefaultRoot+")")
	fl.BoolVar(&f.apply, "apply", false, "do the move (without it nothing changes)")
	fl.BoolVar(&f.reverse, "reverse", false, "move <source>/raw back to raw/<source>")
	fl.BoolVar(&f.checksum, "checksum", false, "with --apply, also compare the SHA-256 of every file before and after")
	return cmd
}

// runMigrate resolves the data root and hands the request to the application
// service. Transport only: no provider and no domain package.
func (a *app) runMigrate(cmd *cobra.Command, f migrateFlags) error {
	cfg, err := loadConfig(f.config, cmd.Flags().Changed("config"))
	if err != nil {
		return err
	}
	if err := useRoot(resolveRoot(f.dataRoot, a.getenv(envDataRoot), cfg.Marketdata.Root)); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	report, err := service.MigrateLayout(ctx, logging.WithComponent(slog.Default(), logging.ComponentMarketData),
		service.MigrateRequest{Apply: f.apply, Reverse: f.reverse, Checksum: f.checksum})
	if err != nil && !errors.Is(err, service.ErrMigrationFailed) {
		return err // the request could not run, or was cancelled: there is no report
	}
	out := cmd.OutOrStdout()
	var writeErr error // the first failure to write the report
	say := func(format string, args ...any) {
		if _, werr := fmt.Fprintf(out, format, args...); werr != nil && writeErr == nil {
			writeErr = fmt.Errorf("write report: %w", werr)
		}
	}
	writeMigrationText(say, report)
	return errors.Join(err, writeErr)
}

func writeMigrationText(say func(string, ...any), r service.MigrationReport) {
	say("data root: %s\n", r.Root)
	switch {
	case !r.Applied:
		say("dry run: nothing is moved; run again with --apply to move\n")
	case r.Reverse:
		say("applying (reverse)\n")
	default:
		say("applying\n")
	}
	if len(r.Units) == 0 && len(r.Problems) == 0 {
		say("nothing to migrate\n")
	}
	var files int
	var bytes int64
	for _, u := range r.Units {
		files += u.Files
		bytes += u.Bytes
		say("%s -> %s: %d files, %s [%s]\n", u.From, u.To, u.Files, humanBytes(u.Bytes), u.State)
		for _, n := range u.Notes {
			say("  note: %s\n", n)
		}
		for _, p := range u.Problems {
			say("  PROBLEM: %s\n", p)
		}
	}
	for _, p := range r.Problems {
		say("PROBLEM: %s\n", p)
	}
	if len(r.LegacyCanonical) > 0 {
		say("legacy canonical (canonical-v1, not moved; rebuild with trader data build, delete when trader data audit passes): %s\n",
			strings.Join(r.LegacyCanonical, ", "))
	}
	say("total: %d sources, %d files, %s\n", len(r.Units), files, humanBytes(bytes))
	if r.OK {
		say("migrate: OK\n")
	} else {
		say("migrate: FAILED\n")
	}
}

// humanBytes renders a size for people: 1536 as "1.5 KiB".
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
