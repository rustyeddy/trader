package account

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/cmd/internal/accountactivitycfg"
	"github.com/rustyeddy/trader/internal/accountactivity"
	svc "github.com/rustyeddy/trader/internal/service/accountactivity"
)

func newActivityCmd(d deps, flags *groupFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Sync and list broker account activity.",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newSyncCmd(d, flags))
	cmd.AddCommand(newListCmd(d, flags))
	return cmd
}

// SyncOutput is what a sync prints: the stream, what the run did, and where
// the checkpoint now stands.
type SyncOutput struct {
	Stream        accountactivity.SyncKey
	Result        svc.Result
	Checkpoint    accountactivity.Checkpoint
	HasCheckpoint bool
}

func newSyncCmd(d deps, flags *groupFlags) *cobra.Command {
	var from, to string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Import new activity from the broker into the ledger.",
		Long: "Import everything beyond the stream's checkpoint. A rerun is safe: activity already " +
			"stored is counted as unchanged, and an interrupted run resumes from the last durable page.\n\n" +
			"--from/--to bound an import to a half-open window [from, to), for bootstrapping history.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, t, err := parseRange(from, to)
			if err != nil {
				return err
			}
			if f.IsZero() && !t.IsZero() {
				return errors.New("--to needs --from: a bounded import starts somewhere")
			}
			s, err := d.open(cmd, flags)
			if err != nil {
				return err
			}
			if err := requireActivityFormat(s.formatter); err != nil {
				return err
			}
			src, err := d.newSource(s.cfg, s.stream, accountactivitycfg.Bounds{From: f, To: t}, d.clock)
			if err != nil {
				return err
			}
			res, err := svc.Sync(cmd.Context(), svc.Params{Store: s.store, Source: src, Clock: d.clock})
			if err != nil {
				return fmt.Errorf("sync stopped after %d pages (%d new, %d unchanged): %w", res.Pages, res.Added, res.Unchanged, err)
			}
			cp, ok, err := s.store.Checkpoint(cmd.Context(), s.stream)
			if err != nil {
				return err
			}
			return s.formatter.FormatSync(cmd.OutOrStdout(), SyncOutput{Stream: s.stream, Result: res, Checkpoint: cp, HasCheckpoint: ok})
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "import window start (YYYY-MM-DD or RFC3339), inclusive")
	cmd.Flags().StringVar(&to, "to", "", "import window end (YYYY-MM-DD or RFC3339), exclusive")
	return cmd
}

func newListCmd(d deps, flags *groupFlags) *cobra.Command {
	var from, to, kinds string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored account activity.",
		Long: "List stored activity in ledger order over the half-open range [from, to). " +
			"At most --limit activities are shown (the earliest in the range); the output says " +
			"when more matched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, t, err := parseRange(from, to)
			if err != nil {
				return err
			}
			ks, err := parseKinds(kinds)
			if err != nil {
				return err
			}
			if limit < 0 {
				return fmt.Errorf("--limit %d must not be negative", limit)
			}
			s, err := d.open(cmd, flags)
			if err != nil {
				return err
			}
			if err := requireActivityFormat(s.formatter); err != nil {
				return err
			}
			res, err := svc.List(cmd.Context(), svc.ListParams{Store: s.store, Stream: s.stream, From: f, To: t, Kinds: ks, Limit: limit})
			if err != nil {
				return err
			}
			return s.formatter.FormatList(cmd.OutOrStdout(), ListOutput{Result: res, From: f, To: t})
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "range start (YYYY-MM-DD or RFC3339), inclusive")
	cmd.Flags().StringVar(&to, "to", "", "range end (YYYY-MM-DD or RFC3339), exclusive")
	cmd.Flags().StringVar(&kinds, "kind", "", "only these kinds, comma-separated (fill, financing, fee, deposit, withdrawal, dividend, adjustment, other)")
	cmd.Flags().IntVar(&limit, "limit", svc.DefaultListLimit, "most activities to show")
	return cmd
}

// ListOutput is what a list prints.
type ListOutput struct {
	Result   svc.ListResult
	From, To time.Time
}
