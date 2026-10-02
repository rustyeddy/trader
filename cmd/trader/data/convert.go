package data

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

// newConvertCmd imports one native Stooq archive member into managed raw
// partitions and builds the requested canonical range. The service
// extracts the member to a temporary directory; the original ZIP remains
// the source of truth.
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
			if strings.ToLower(dc.Provider) != "stooq" {
				return fmt.Errorf("convert currently supports only provider stooq")
			}
			req, err := resolveDatasetRequest(cmd, args, flags)
			if err != nil {
				return err
			}
			if strings.ToUpper(args[1]) != "D1" {
				return fmt.Errorf("stooq conversion currently supports only D1")
			}
			resp, err := dc.Service.ConvertStooqArchive(cmd.Context(), svc.ConvertStooqArchiveRequest{
				DatasetRequest: req, Symbol: args[0], ArchivePath: archivePath,
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "imported %d rows across %d raw months; published %d canonical partitions\n",
				resp.Import.RowsImported, resp.Import.MonthsWritten, len(resp.Build.Result.Published)); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&archivePath, "archive", "", "native Stooq ZIP archive path")
	addDatasetConversionFlags(cmd, &flags)
	return cmd
}
