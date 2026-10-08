package marketdata

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	// ErrIncompleteListingIdentity reports an exchange without a kind, or
	// a kind without an exchange. It is returned wrapped together with
	// ErrInvalidRequest.
	ErrIncompleteListingIdentity = errors.New("exchange and kind must be provided together")
	// ErrInvalidListingKind reports a kind other than KindEquity or
	// KindETF. It is returned wrapped together with ErrInvalidRequest.
	ErrInvalidListingKind = errors.New("invalid listing kind")
)

// Listing kinds a ListingDefault may name.
const (
	KindEquity = "equity"
	KindETF    = "etf"
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
	"SPY":  {Exchange: "ARCA", Kind: KindETF},
	"QQQ":  {Exchange: "NASDAQ", Kind: KindETF},
	"AAPL": {Exchange: "NASDAQ", Kind: KindEquity},
}

// DefaultListing returns the reference listing default for symbol
// (case-insensitive), if Trader has one.
func DefaultListing(symbol string) (ListingDefault, bool) {
	d, ok := listingDefaults[strings.ToUpper(strings.TrimSpace(symbol))]
	return d, ok
}

// ResolveListingIdentity returns the exchange and kind to register symbol
// under. Explicit values must be given together and win; the kind is
// normalized to KindEquity or KindETF and any other kind fails with
// ErrInvalidListingKind (and ErrInvalidRequest). With neither, the symbol's DefaultListing applies,
// and a symbol without one fails with ErrNoListingDefault.
func ResolveListingIdentity(symbol, exchange, kind string) (ListingDefault, error) {
	exchange, kind = strings.TrimSpace(exchange), strings.ToLower(strings.TrimSpace(kind))
	switch {
	case exchange != "" && kind != "":
		if kind != KindEquity && kind != KindETF {
			return ListingDefault{}, fmt.Errorf("%w: %w %q: expected %q or %q", ErrInvalidRequest, ErrInvalidListingKind, kind, KindEquity, KindETF)
		}
		return ListingDefault{Exchange: exchange, Kind: kind}, nil
	case exchange != "" || kind != "":
		return ListingDefault{}, fmt.Errorf("%w: %w", ErrInvalidRequest, ErrIncompleteListingIdentity)
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
	// ArchivePath is the native Stooq ZIP. When empty, the archive under
	// ArchiveRoot is found by FindStooqArchive: the one whose name
	// contains Symbol, else the one multi-symbol bundle (d_us_txt.zip)
	// holding Symbol's daily member.
	ArchivePath string
	// ArchiveRoot is searched when ArchivePath is empty. When it is empty
	// too, the Service's own archive root (WithArchiveRoot) is used.
	ArchiveRoot string
	// Force rebuilds canonical partitions even when they are current and
	// re-imports the whole export instead of merging only the rows after
	// the last raw date.
	Force bool
}

// ConvertStooqArchive locates the archive, extracts the symbol's member
// to a temporary directory it removes before returning, and converts it
// the way Convert does. The archive itself is never modified. Once the
// request validates, the operation logs exactly one outcome record,
// covering discovery and extraction failures too.
func (s *Service) ConvertStooqArchive(ctx context.Context, req ConvertStooqArchiveRequest) (resp ConvertResponse, err error) {
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
	defer func() {
		s.logOutcome(ctx, slog.LevelInfo, "stooq archive convert completed", "stooq archive convert failed", req.DatasetRequest, err,
			"symbol", symbol, "rows_imported", resp.Import.RowsImported, "rows_added", resp.Import.RowsAdded, "full_reimport", resp.Import.FullReimport, "published_partitions", len(resp.Build.Result.Published))
	}()
	archivePath := req.ArchivePath
	if archivePath == "" {
		root := req.ArchiveRoot
		if root == "" {
			root = s.archiveRoot
		}
		if archivePath, err = FindStooqArchive(ctx, root, symbol); err != nil {
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
	return s.convert(ctx, convert)
}

// ConvertRequest returns the Convert request req describes, without an
// archive path.
func (req ConvertStooqArchiveRequest) ConvertRequest() ConvertRequest {
	return ConvertRequest{DatasetRequest: req.DatasetRequest, Force: req.Force}
}

// FindStooqArchive returns the ZIP under root that holds symbol's daily
// data, searching in this order:
//
//  1. The one ZIP whose file name contains symbol as a '_', '.', or '-'
//     separated token (case-insensitive), such as spy_us_d.zip for SPY.
//  2. When no name matches, the one other ZIP under root with a daily
//     <symbol>.us.txt member, such as Stooq's multi-symbol bundle
//     d_us_txt.zip (data/daily/us/nyse etfs/2/spy.us.txt). Members under
//     an intraday directory (h_us_txt.zip's data/hourly/...) never match.
//     A file that cannot be read as a ZIP is skipped.
//
// More than one match at either step is ErrAmbiguousArchive; none is
// ErrArchiveNotFound. The walk stops with ctx's error once ctx is done.
func FindStooqArchive(ctx context.Context, root, symbol string) (string, error) {
	if root == "" {
		return "", ErrArchiveRootNotConfigured
	}
	symbol = strings.ToLower(strings.TrimSpace(symbol))
	var named, others []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		switch {
		case !strings.HasSuffix(name, ".zip"):
		case archiveNameHasSymbol(name, symbol):
			named = append(named, path)
		default:
			others = append(others, path)
		}
		return nil
	})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if err != nil {
		return "", fmt.Errorf("search Stooq archive root %q: %w", root, err)
	}
	matches := named
	if len(matches) == 0 {
		for _, path := range others {
			has, err := zipHasDailyMember(ctx, path, symbol)
			if err != nil {
				return "", err
			}
			if has {
				matches = append(matches, path)
			}
		}
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

// zipHasDailyMember reports whether the ZIP at path holds a daily member
// for symbol. A file that is not a readable ZIP reports false.
func zipHasDailyMember(ctx context.Context, path, symbol string) (bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, ctx.Err()
	}
	defer func() { _ = zr.Close() }()
	for _, entry := range zr.File {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if isStooqDailyMember(entry, symbol) {
			return true, nil
		}
	}
	return false, nil
}

// intradayDirs are the directory names Stooq's bundles use for non-daily
// periods; a member below one of them holds rows ConvertStooqArchive
// cannot import.
var intradayDirs = map[string]bool{"hourly": true, "5 min": true}

// isStooqDailyMember reports whether entry is symbol's <symbol>.us.txt
// file and does not sit below an intraday directory.
func isStooqDailyMember(entry *zip.File, symbol string) bool {
	if entry.FileInfo().IsDir() {
		return false
	}
	parts := strings.Split(strings.ToLower(entry.Name), "/")
	if parts[len(parts)-1] != strings.ToLower(strings.TrimSpace(symbol))+".us.txt" {
		return false
	}
	for _, dir := range parts[:len(parts)-1] {
		if intradayDirs[dir] {
			return false
		}
	}
	return true
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
		if !isStooqDailyMember(entry, symbol) {
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
		_, copyErr := io.Copy(out, ctxReader{ctx: ctx, r: in})
		closeInErr := in.Close()
		closeOutErr := out.Close()
		if copyErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return "", ctxErr
			}
			return "", fmt.Errorf("extract archive member %q: %w", entry.Name, copyErr)
		}
		if closeInErr != nil || closeOutErr != nil {
			return "", fmt.Errorf("close extracted archive member %q: %v %v", entry.Name, closeInErr, closeOutErr)
		}
		return path, nil
	}
	return "", fmt.Errorf("%w: archive %q contains no %s member", ErrArchiveMemberNotFound, archivePath, want)
}

// ctxReader is an io.Reader that fails with ctx's error once ctx is done,
// so a long copy stops promptly on cancellation.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
