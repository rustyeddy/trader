package data

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

// singleResult returns the one result of a one-instrument multi-symbol
// operation (issue #439): the CLI's build, update, convert, and stq2bars
// commands run the same service operations as MCP, one symbol at a time.
func singleResult(resp svc.DatasetsResponse, err error) (svc.DatasetResult, error) {
	if err != nil && len(resp.Results) == 0 {
		return svc.DatasetResult{}, err
	}
	if len(resp.Results) != 1 {
		return svc.DatasetResult{}, fmt.Errorf("expected one dataset result, got %d", len(resp.Results))
	}
	res := resp.Results[0]
	if res.Err == nil {
		res.Err = err
	}
	return res, nil
}

// formatDatasetResult prints res's step with the command's formatter and
// returns res.Err, joined with any formatting failure (never masking it).
// An update renders as an update, partial progress only when it failed
// (FormatUpdateProgress); a build or archive conversion renders its
// build. With no step to show (nothing to update), it prints the status.
func formatDatasetResult(w io.Writer, formatter Formatter, res svc.DatasetResult) error {
	var formatErr error
	switch {
	case res.Update != nil && res.Err != nil:
		formatErr = formatter.FormatUpdateProgress(w, *res.Update)
	case res.Update != nil:
		formatErr = formatter.FormatUpdate(w, *res.Update)
	case res.Build != nil:
		formatErr = formatter.FormatBuild(w, *res.Build)
	case res.Convert != nil:
		formatErr = formatter.FormatBuild(w, res.Convert.Build)
	case res.Err == nil:
		_, formatErr = fmt.Fprintf(w, "%s: %s\n", res.Request.Symbol, res.Status)
	}
	if res.Err != nil {
		if formatErr != nil {
			return errors.Join(res.Err, formatErr)
		}
		return res.Err
	}
	return formatErr
}

// optionalRangeUsage documents --from/--to as optional on cmd, saying
// what omitting both means.
func optionalRangeUsage(cmd *cobra.Command, omitted string) {
	cmd.Flags().Lookup("from").Usage = "range start (YYYY-MM-DD or RFC3339); with --to, or omit both for " + omitted
	cmd.Flags().Lookup("to").Usage = "range end (YYYY-MM-DD or RFC3339); with --from, or omit both for " + omitted
}
