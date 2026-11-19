package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rustyeddy/trader/internal/marketdata/layout"
	"github.com/rustyeddy/trader/internal/store"
)

// ErrMigrationFailed reports that MigrateLayout found something that stops it, or
// that a move or its verification failed. The MigrationReport returned with it
// says what.
var ErrMigrationFailed = errors.New("service: layout migration failed")

// MigrateRequest asks to move the raw archive from the old layout
// (<root>/raw/<source>/...) to the one the code reads (<root>/<source>/raw/...).
type MigrateRequest struct {
	// Apply carries the moves out. Without it the request is a dry run: it plans
	// and checks and changes nothing.
	Apply bool
	// Reverse moves the other way, <source>/raw back to raw/<source>.
	Reverse bool
	// Checksum, with Apply, also proves every file's bytes unchanged: SHA-256 of
	// each file before and after. Without it a file is proved to be the same file
	// by its inode, size and modification time, which a rename cannot change.
	Checksum bool
}

// Migration unit states.
const (
	// MigratePlanned: found, checked, not moved (a dry run, or a refused plan).
	MigratePlanned = "planned"
	// MigrateVerified: moved, and every file is where and what it was.
	MigrateVerified = "verified"
	// MigrateFailed: the move failed, or the files after it are not the files before.
	MigrateFailed = "failed"
)

// MigrationUnit is one source's directory.
type MigrationUnit struct {
	Source string
	// From and To are root-relative directories.
	From, To string
	Files    int
	Bytes    int64
	State    string
	// Problems are what stops this unit from moving, or what verification found.
	Problems []string
	// Notes are things worth knowing that do not stop it (such as partial downloads).
	Notes []string
}

// MigrationReport is everything a migration did or found.
type MigrationReport struct {
	// Root is the data root the migration ran under.
	Root    string
	Reverse bool
	Applied bool
	Units   []MigrationUnit
	// Problems are findings that are not about one unit.
	Problems []string
	// LegacyCanonical lists the old canonical/<source> directories. They are
	// canonical-v1, are never moved, and are rebuilt with trader data build; the
	// operator deletes them once the rebuild has been audited.
	LegacyCanonical []string
	OK              bool
}

// MigrateLayout moves the raw archive between the old layout and the current one by
// renaming each source's directory as a whole. Nothing is copied: on one
// filesystem a rename is atomic and as fast for a million files as for one, and
// every file keeps its inode and its bytes. It plans first and checks the plan, and
// moves nothing if any check fails:
//
//   - the destination of a unit must not exist (nothing is overwritten or merged);
//   - a source directory holds no symbolic link and none of the store's temporary
//     files (one means a download or build is running or was interrupted);
//   - every name is a valid path component.
//
// Then, for an applied request, it moves unit by unit and verifies each: the files
// after are exactly the files before (same set, sizes, modification times and
// inodes, and with Checksum the same SHA-256). A unit that is already moved is no
// longer in the old place, so a rerun after an interruption moves what is left. A
// verification failure is reported and nothing is moved back automatically: the
// operator decides, and Reverse undoes the move with the same checks.
//
// The canonical directory is never touched: the old files are canonical-v1, which
// are rebuilt from raw (ADR-090), and the report lists them. Only the store
// reads and writes files. A report is always returned with ErrMigrationFailed; a
// cancellation or a failure to read returns the error alone. log may be nil.
func MigrateLayout(ctx context.Context, log *slog.Logger, req MigrateRequest) (MigrationReport, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if req.Checksum && !req.Apply {
		return MigrationReport{}, errors.New("migrate: --checksum needs --apply (a dry run reads no file contents)")
	}
	report := MigrationReport{Root: store.Root(), Reverse: req.Reverse, Applied: req.Apply, OK: true}

	var err error
	var units []MigrationUnit
	if req.Reverse {
		units, err = reverseUnits(&report)
	} else {
		units, err = forwardUnits(&report)
	}
	if err != nil {
		return MigrationReport{}, err
	}
	befores := make([][]store.Entry, len(units))
	for i := range units {
		if err := ctx.Err(); err != nil {
			return MigrationReport{}, err
		}
		entries, err := planUnit(ctx, &units[i])
		if err != nil {
			return MigrationReport{}, err
		}
		befores[i] = entries
	}
	report.Units = units
	for _, u := range units {
		if len(u.Problems) > 0 {
			report.OK = false
		}
	}
	if len(report.Problems) > 0 {
		report.OK = false
	}
	if !req.Reverse {
		legacy, err := store.Subdirs(layout.LegacyCanonicalRoot)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return MigrationReport{}, err
		}
		report.LegacyCanonical = legacy
	}
	if !report.OK {
		return report, migrationFailed(report)
	}
	if !req.Apply {
		return report, nil
	}

	for i := range report.Units {
		u := &report.Units[i]
		var sums map[string]string
		if req.Checksum {
			if sums, err = checksums(ctx, befores[i]); err != nil {
				return report, err
			}
		}
		log.Info("moving", "from", u.From, "to", u.To, "files", u.Files)
		if err := store.Move(ctx, u.From, u.To); err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return report, cerr
			}
			u.State = MigrateFailed
			u.Problems = append(u.Problems, "move: "+err.Error())
			report.OK = false
			return report, migrationFailed(report)
		}
		problems, err := verifyMoved(ctx, u, befores[i], sums, req.Checksum)
		if err != nil {
			return report, err
		}
		if len(problems) > 0 {
			u.State, u.Problems = MigrateFailed, problems
			report.OK = false
			log.Error("verification failed", "from", u.From, "to", u.To, "problems", len(problems))
			return report, migrationFailed(report)
		}
		u.State = MigrateVerified
		log.Info("moved and verified", "to", u.To, "files", u.Files)
	}
	return report, nil
}

func migrationFailed(r MigrationReport) error {
	n := len(r.Problems)
	for _, u := range r.Units {
		n += len(u.Problems)
	}
	return fmt.Errorf("%w: %d problems", ErrMigrationFailed, n)
}

// forwardUnits lists raw/<source> for every source directory under the old raw root.
func forwardUnits(report *MigrationReport) ([]MigrationUnit, error) {
	sources, err := store.Subdirs(layout.LegacyRawRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // nothing at the old place: already migrated, or never there
	}
	if err != nil {
		return nil, err
	}
	var units []MigrationUnit
	for _, s := range sources {
		from, to, lerr := layout.LegacyRawDir(s)
		if lerr != nil {
			report.Problems = append(report.Problems, fmt.Sprintf("%s/%s: %v", layout.LegacyRawRoot, s, lerr))
			continue
		}
		units = append(units, MigrationUnit{Source: s, From: from, To: to, State: MigratePlanned})
	}
	return units, nil
}

// reverseUnits lists <source>/raw for every top-level directory that has one.
func reverseUnits(report *MigrationReport) ([]MigrationUnit, error) {
	tops, err := store.Subdirs("")
	if err != nil {
		return nil, err
	}
	var units []MigrationUnit
	for _, s := range tops {
		if s == layout.LegacyRawRoot || s == layout.LegacyCanonicalRoot {
			continue
		}
		to, from, lerr := layout.LegacyRawDir(s) // the same pair, the other way round
		if lerr != nil {
			continue // not a source name: not ours
		}
		if ok, err := store.Exists(from); err != nil {
			return nil, err
		} else if !ok {
			continue
		}
		units = append(units, MigrationUnit{Source: s, From: from, To: to, State: MigratePlanned})
	}
	return units, nil
}

// planUnit inventories the unit's source directory and records what stops it. It
// returns the inventory, which verification compares against.
func planUnit(ctx context.Context, u *MigrationUnit) ([]store.Entry, error) {
	if ok, err := store.Exists(u.To); err != nil {
		return nil, err
	} else if ok {
		u.Problems = append(u.Problems, fmt.Sprintf("the destination %s exists: nothing is overwritten or merged", u.To))
	}
	entries, err := store.Inventory(ctx, u.From)
	if err != nil {
		return nil, fmt.Errorf("inventory %s: %w", u.From, err)
	}
	partial := 0
	for _, e := range entries {
		u.Files++
		u.Bytes += e.Size
		switch {
		case e.Symlink:
			u.Problems = append(u.Problems, fmt.Sprintf("%s is a symbolic link: it is never followed or moved blindly", e.Path))
		case store.IsTemp(e.Path):
			u.Problems = append(u.Problems, fmt.Sprintf("%s is a temporary file: a download or build is running or was interrupted", e.Path))
		case strings.HasSuffix(e.Path, ".part"):
			partial++
		}
	}
	if partial > 0 {
		u.Notes = append(u.Notes, fmt.Sprintf("%d partial downloads (.part) move with the rest and are not touched", partial))
	}
	return entries, nil
}

// moved is where a file that was at path under the unit's From is after the move.
func moved(u *MigrationUnit, path string) string {
	return filepath.Join(u.To, strings.TrimPrefix(path, u.From+string(filepath.Separator)))
}

func checksums(ctx context.Context, entries []store.Entry) (map[string]string, error) {
	sums := make(map[string]string, len(entries))
	for _, e := range entries {
		sum, err := store.Checksum(ctx, e.Path)
		if err != nil {
			return nil, err
		}
		sums[e.Path] = sum
	}
	return sums, nil
}

// maxProblems bounds how many differences a verification lists.
const maxProblems = 20

// verifyMoved compares the files now at u.To with the inventory taken at u.From.
func verifyMoved(ctx context.Context, u *MigrationUnit, before []store.Entry, sums map[string]string, checksum bool) ([]string, error) {
	after, err := store.Inventory(ctx, u.To)
	if err != nil {
		return nil, fmt.Errorf("inventory %s: %w", u.To, err)
	}
	var problems []string
	add := func(format string, args ...any) {
		if len(problems) < maxProblems {
			problems = append(problems, fmt.Sprintf(format, args...))
		}
	}
	byPath := make(map[string]store.Entry, len(after))
	for _, e := range after {
		byPath[e.Path] = e
	}
	if len(after) != len(before) {
		add("%d files before the move, %d after", len(before), len(after))
	}
	for _, b := range before {
		to := moved(u, b.Path)
		a, ok := byPath[to]
		switch {
		case !ok:
			add("%s is missing after the move", to)
		case a.Size != b.Size:
			add("%s: size %d, was %d", to, a.Size, b.Size)
		case !a.ModTime.Equal(b.ModTime):
			add("%s: modification time changed", to)
		case a.Inode != 0 && b.Inode != 0 && a.Inode != b.Inode:
			add("%s: not the file that was at %s (inode differs)", to, b.Path)
		}
		if checksum && ok {
			sum, err := store.Checksum(ctx, to)
			if err != nil {
				return nil, err
			}
			if sum != sums[b.Path] {
				add("%s: contents differ from before the move (SHA-256)", to)
			}
		}
		delete(byPath, to)
	}
	extras := make([]string, 0, len(byPath))
	for extra := range byPath {
		extras = append(extras, extra)
	}
	sort.Strings(extras)
	for _, extra := range extras {
		add("%s appeared that was not in the source", extra)
	}
	return problems, nil
}
