package data

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

// newConvertCmd imports one native Stooq archive member into managed raw
// partitions and builds the requested canonical range, through the
// service's CanonicalizeDatasets (issue #439). The service extracts the
// member to a temporary directory; the original ZIP remains the source of
// truth.
func newConvertCmd() *cobra.Command {
	var flags datasetArgFlags
	var archivePath string

	cmd := &cobra.Command{
		Use:   "convert INSTRUMENT INTERVAL",
		Short: "Convert one downloaded provider archive into canonical data.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if archivePath == "" {
				return fmt.Errorf("--archive is required")
			}
			dc, ok := dataContextFrom(cmd.Context())
			if !ok {
				return fmt.Errorf("data service is not configured on this command's context")
			}
			if dc.Service.ProviderInfo().Archive != marketruntime.ArchiveStooqZIP {
				return fmt.Errorf("convert currently supports only provider stooq")
			}
			req, err := parseDatasetsRequest(args, flags, false)
			if err != nil {
				return err
			}
			if strings.ToUpper(args[1]) != "D1" {
				return fmt.Errorf("stooq conversion currently supports only D1")
			}
			// The same canonicalize operation MCP runs (issue #439), for
			// one symbol and this archive.
			res, err := singleResult(dc.Service.CanonicalizeDatasets(cmd.Context(), req,
				svc.CanonicalizeOptions{ArchivePath: archivePath}))
			if err != nil {
				return err
			}
			if res.Err != nil {
				return instrumentFlagError(res.Err, dc.Provider, flags)
			}
			imported := 0
			months := 0
			note := ""
			if res.Convert != nil {
				imported, months = res.Convert.Import.RowsAdded, res.Convert.Import.MonthsWritten
				note = fullReimportNote(res.Convert.Import.FullReimport)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "imported %d rows across %d raw months%s; published %d canonical partitions\n",
				imported, months, note, res.PublishedPartitions)
			return err
		},
	}
	cmd.Flags().StringVar(&archivePath, "archive", "", "native Stooq ZIP archive path")
	addDatasetConversionFlags(cmd, &flags)
	return cmd
}
