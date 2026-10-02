package marketdata

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rustyeddy/trader/marketdata"
)

// Stooq archive errors. Transports may add their own guidance (for
// example naming a flag) by matching these with errors.Is.
var (
	// ErrArchiveRootNotConfigured reports an archive search with no
	// archive root to search.
	ErrArchiveRootNotConfigured = errors.New("stooq archive root is not configured")
	// ErrArchiveNotFound reports that no archive for the symbol exists
	// under the archive root.
	ErrArchiveNotFound = errors.New("stooq archive not found")
	// ErrAmbiguousArchive reports more than one candidate archive for the
	// symbol under the archive root.
	ErrAmbiguousArchive = errors.New("multiple stooq archives found")
	// ErrArchiveMemberNotFound reports an archive with no member for the
	// requested symbol.
	ErrArchiveMemberNotFound = errors.New("stooq archive member not found")
	// ErrNoListingDefault reports a symbol with no reference listing
	// default, so its exchange and kind must be supplied explicitly.
	ErrNoListingDefault = errors.New("no listing default for symbol")
)

// ListingDefault is the exchange and kind ("equity" or "etf") of a
// reference listing Trader can resolve from its symbol alone.
type ListingDefault struct {
	Exchange string
	Kind     string
}

// listingDefaults is the small set of reference listings Trader resolves
// without asking the caller to repeat catalog metadata. Symbols outside it
// need an explicit exchange and kind, so nothing guesses an instrument
// identity from a symbol.
var listingDefaults = map[string]ListingDefault{
	"SPY":  {Exchange: "ARCA", Kind: "etf"},
	"QQQ":  {Exchange: "NASDAQ", Kind: "etf"},
	"AAPL": {Exchange: "NASDAQ", Kind: "equity"},
}

// DefaultListing returns the reference listing default for symbol
// (case-insensitive), if Trader has one.
func DefaultListing(symbol string) (ListingDefault, bool) {
	d, ok := listingDefaults[strings.ToUpper(strings.TrimSpace(symbol))]
	return d, ok
}

// ResolveListingIdentity returns the exchange and kind to register symbol
// under. Explicit values must be given together and win; with neither,
// the symbol's DefaultListing applies, and a symbol without one fails
// with ErrNoListingDefault.
func ResolveListingIdentity(symbol, exchange, kind string) (ListingDefault, error) {
	switch {
	case exchange != "" && kind != "":
		return ListingDefault{Exchange: exchange, Kind: kind}, nil
	case exchange != "" || kind != "":
		return ListingDefault{}, fmt.Errorf("%w: exchange and kind must be provided together", ErrInvalidRequest)
	}
	d, ok := DefaultListing(symbol)
	if !ok {
		return ListingDefault{}, fmt.Errorf("%w: %q", ErrNoListingDefault, strings.ToUpper(strings.TrimSpace(symbol)))
	}
	return d, nil
}

// ConvertStooqArchiveRequest converts one symbol from a native Stooq ZIP
// archive into canonical D1 bars.
type ConvertStooqArchiveRequest struct {
	// DatasetRequest names the dataset. Instrument must already be
	// registered under provider stooq; Interval must be D1. A zero Range
	// converts everything the archive holds for the symbol; otherwise
	// the range is clipped to the source data.
	DatasetRequest
	// Symbol is the Stooq ticker. It selects the archive's
	// <symbol>.us.txt member and, when ArchivePath is empty, the archive
	// itself.
	Symbol string
	// ArchivePath is the native Stooq ZIP. When empty, the one archive
	// under ArchiveRoot whose name contains Symbol is used.
	ArchivePath string
	// ArchiveRoot is searched when ArchivePath is empty.
	ArchiveRoot string
	// Force rebuilds canonical partitions even when they are current.
	Force bool
}

// ConvertStooqArchive locates the archive, extracts the symbol's member
// to a temporary directory it removes before returning, and converts it
// through Convert. The archive itself is never modified.
func (s *Service) ConvertStooqArchive(ctx context.Context, req ConvertStooqArchiveRequest) (ConvertResponse, error) {
	if err := validateConvertRequest(req.ConvertRequest()); err != nil {
		return ConvertResponse{}, err
	}
	if req.Interval != marketdata.D1 {
		return ConvertResponse{}, fmt.Errorf("%w: stooq conversion supports only D1", ErrInvalidRequest)
	}
	symbol := strings.TrimSpace(req.Symbol)
	if symbol == "" {
		return ConvertResponse{}, fmt.Errorf("%w: symbol is required", ErrInvalidRequest)
	}
	archivePath := req.ArchivePath
	if archivePath == "" {
		var err error
		if archivePath, err = FindStooqArchive(req.ArchiveRoot, symbol); err != nil {
			return ConvertResponse{}, err
		}
	}
	tmp, err := os.MkdirTemp("", "trader-stooq-")
	if err != nil {
		return ConvertResponse{}, fmt.Errorf("create temporary extraction directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	extracted, err := extractStooqMember(ctx, archivePath, symbol, tmp)
	if err != nil {
		return ConvertResponse{}, err
	}
	convert := req.ConvertRequest()
	convert.ArchivePath = extracted
	return s.Convert(ctx, convert)
}

// ConvertRequest returns the Convert request req describes, without an
// archive path.
func (req ConvertStooqArchiveRequest) ConvertRequest() ConvertRequest {
	return ConvertRequest{DatasetRequest: req.DatasetRequest, Force: req.Force}
}

// FindStooqArchive returns the one ZIP under root whose file name contains
// symbol as a '_', '.', or '-' separated token (case-insensitive), such as
// spy_us_d.zip for SPY.
func FindStooqArchive(root, symbol string) (string, error) {
	if root == "" {
		return "", ErrArchiveRootNotConfigured
	}
	symbol = strings.ToLower(strings.TrimSpace(symbol))
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".zip") && archiveNameHasSymbol(name, symbol) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search Stooq archive root %q: %w", root, err)
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%w: %s under %q", ErrArchiveNotFound, strings.ToUpper(symbol), root)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%w: %s under %q", ErrAmbiguousArchive, strings.ToUpper(symbol), root)
	}
}

func archiveNameHasSymbol(name, symbol string) bool {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(name)), ".zip")
	tokens := strings.FieldsFunc(base, func(r rune) bool { return r == '_' || r == '.' || r == '-' })
	return slices.Contains(tokens, strings.ToLower(symbol))
}

// extractStooqMember copies the archive's <symbol>.us.txt member into
// destination and returns the extracted file's path.
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
	return "", fmt.Errorf("%w: archive %q contains no %s member", ErrArchiveMemberNotFound, archivePath, want)
}
