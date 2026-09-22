package data

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

// newConvertCmd imports one native Stooq archive member into managed raw
// partitions and builds the requested canonical range. Extraction is always
// temporary; the original ZIP remains the source of truth.
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
			tmp, err := os.MkdirTemp("", "trader-stooq-convert-")
			if err != nil {
				return fmt.Errorf("create temporary extraction directory: %w", err)
			}
			defer func() { _ = os.RemoveAll(tmp) }()
			extracted, err := extractStooqMember(cmd.Context(), archivePath, args[0], tmp)
			if err != nil {
				return err
			}
			resp, err := dc.Service.Convert(cmd.Context(), svc.ConvertRequest{
				DatasetRequest: req, ArchivePath: extracted,
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

func extractStooqMember(ctx context.Context, archivePath, symbol, destination string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = zr.Close() }()
	want := strings.ToLower(strings.TrimSpace(symbol)) + ".us.txt"
	for _, entry := range zr.File {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if strings.ToLower(filepath.Base(entry.Name)) != want {
			continue
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		in, err := entry.Open()
		if err != nil {
			return "", fmt.Errorf("open archive member %q: %w", entry.Name, err)
		}
		path := filepath.Join(destination, filepath.Base(entry.Name))
		out, err := os.Create(path)
		if err != nil {
			_ = in.Close()
			return "", fmt.Errorf("create extracted archive member: %w", err)
		}
		_, copyErr := io.Copy(out, in)
		closeInErr := in.Close()
		closeOutErr := out.Close()
		if copyErr != nil {
			return "", fmt.Errorf("extract archive member %q: %w", entry.Name, copyErr)
		}
		if closeInErr != nil || closeOutErr != nil {
			return "", fmt.Errorf("close extracted archive member %q: %v %v", entry.Name, closeInErr, closeOutErr)
		}
		return path, nil
	}
	return "", fmt.Errorf("archive %q contains no %s member", archivePath, want)
}
