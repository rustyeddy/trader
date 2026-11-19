package layout

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Stage is the processing stage of a dataset.
type Stage string

// The stages the layout knows.
const (
	// Raw is what the provider gave: for most sources raw-v1 partitions, for a
	// source that ships archives (stooq) the downloaded archive files.
	Raw Stage = "raw"
	// Unzipped is the intermediate stage of a source that ships archives holding many
	// instruments and timeframes: the archives' contents split per instrument and
	// month. Only such a source has it; the others go from Raw to Canonical.
	Unzipped Stage = "unzipped"
	// Canonical is the validated, source-independent form built from the stage before.
	Canonical Stage = "canonical"
)

// Valid reports whether s is exactly Raw, Unzipped or Canonical.
func (s Stage) Valid() bool { return s == Raw || s == Unzipped || s == Canonical }

// Errors, classifiable with errors.Is.
var (
	// ErrInvalidComponent reports an identifier that cannot be a path component.
	ErrInvalidComponent = errors.New("layout: invalid path component")
	// ErrNotPartitionPath reports a path that is not a partition file under the root.
	ErrNotPartitionPath = errors.New("layout: not a partition path")
)

const fileExt = ".csv"

// Partition identifies one monthly partition file. Source is the provider
// (for example "oanda"), Instrument the provider's symbol as it is spelled in
// the path (for example "EURUSD"), and Interval a lower-case token such as
// "h1" or "d1".
type Partition struct {
	Source     string
	Stage      Stage
	Instrument string
	Year       int
	Month      time.Month
	Interval   string
}

// Validate reports ErrInvalidComponent unless every field can be a path
// component: source and instrument are non-empty, made of letters, digits,
// '.', '_' and '-', and do not begin with a dot; the stage is raw or canonical;
// the interval is lower-case letters and digits; the year has four digits and
// the month is 1 to 12.
func (p Partition) Validate() error {
	if err := validComponent("source", p.Source); err != nil {
		return err
	}
	if !p.Stage.Valid() {
		return fmt.Errorf("%w: stage %q is not raw, unzipped or canonical", ErrInvalidComponent, p.Stage)
	}
	if err := validComponent("instrument", p.Instrument); err != nil {
		return err
	}
	if p.Year < 1000 || p.Year > 9999 {
		return fmt.Errorf("%w: year %d is not four digits", ErrInvalidComponent, p.Year)
	}
	if p.Month < time.January || p.Month > time.December {
		return fmt.Errorf("%w: month %d is not 1 to 12", ErrInvalidComponent, int(p.Month))
	}
	if p.Interval == "" || !allOf(p.Interval, func(c byte) bool { return isLower(c) || isDigit(c) }) {
		return fmt.Errorf("%w: interval %q is not lower-case letters and digits", ErrInvalidComponent, p.Interval)
	}
	return nil
}

// Dir returns the partition's directory, relative to the data root.
func (p Partition) Dir() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	base, err := StageDir(p.Source, p.Stage)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, p.Instrument, fmt.Sprintf("%04d", p.Year), fmt.Sprintf("%02d", int(p.Month))), nil
}

// FileName returns the partition's file name.
func (p Partition) FileName() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%04d-%02d-%s%s", p.Instrument, p.Year, int(p.Month), p.Interval, fileExt), nil
}

// Path returns the partition file's path, relative to the data root (the root
// itself belongs to the store package).
func (p Partition) Path() (string, error) {
	dir, err := p.Dir()
	if err != nil {
		return "", err
	}
	name, err := p.FileName()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// StageDir returns the directory that holds every instrument of one source and
// stage, relative to the data root: <source>/<stage>.
func StageDir(source string, stage Stage) (string, error) {
	if err := validComponent("source", source); err != nil {
		return "", err
	}
	if !stage.Valid() {
		return "", fmt.Errorf("%w: stage %q is not raw, unzipped or canonical", ErrInvalidComponent, stage)
	}
	return filepath.Join(source, string(stage)), nil
}

// Parse is the inverse of Path: it returns the Partition a root-relative path
// names, or ErrNotPartitionPath when the path is not exactly
// <source>/<stage>/<instrument>/<YYYY>/<MM>/<file> with a file name that agrees
// with the directories.
func Parse(path string) (Partition, error) {
	rel := filepath.Clean(path)
	if rel == "." || !filepath.IsLocal(rel) {
		return Partition{}, fmt.Errorf("%w: %s is not relative to the data root", ErrNotPartitionPath, path)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 6 {
		return Partition{}, fmt.Errorf("%w: %s has %d components under the root, want 6", ErrNotPartitionPath, path, len(parts))
	}
	year, yerr := strconv.Atoi(parts[3])
	month, merr := strconv.Atoi(parts[4])
	// Atoi accepts a sign and other spellings; only the exact canonical
	// renderings Path produces are partitions, so Parse inverts Path.
	if yerr != nil || merr != nil || parts[3] != fmt.Sprintf("%04d", year) || parts[4] != fmt.Sprintf("%02d", month) {
		return Partition{}, fmt.Errorf("%w: %s: year %q or month %q is malformed", ErrNotPartitionPath, path, parts[3], parts[4])
	}
	name := parts[5]
	if !strings.HasSuffix(name, fileExt) {
		return Partition{}, fmt.Errorf("%w: %s is not a %s file", ErrNotPartitionPath, path, fileExt)
	}
	stem := strings.TrimSuffix(name, fileExt)
	i := strings.LastIndexByte(stem, '-')
	if i < 0 {
		return Partition{}, fmt.Errorf("%w: file name %q has no interval", ErrNotPartitionPath, name)
	}
	p := Partition{
		Source: parts[0], Stage: Stage(parts[1]), Instrument: parts[2],
		Year: year, Month: time.Month(month), Interval: stem[i+1:],
	}
	if err := p.Validate(); err != nil {
		return Partition{}, fmt.Errorf("%w: %s: %v", ErrNotPartitionPath, path, err)
	}
	want, _ := p.FileName()
	if want != name {
		return Partition{}, fmt.Errorf("%w: file name %q does not match its directories (want %q)", ErrNotPartitionPath, name, want)
	}
	return p, nil
}

func validComponent(what, s string) error {
	switch {
	case s == "":
		return fmt.Errorf("%w: %s is empty", ErrInvalidComponent, what)
	case s[0] == '.':
		return fmt.Errorf("%w: %s %q begins with a dot", ErrInvalidComponent, what, s)
	case !allOf(s, func(c byte) bool { return isLower(c) || isUpper(c) || isDigit(c) || c == '.' || c == '_' || c == '-' }):
		return fmt.Errorf("%w: %s %q has a character outside letters, digits, '.', '_' and '-'", ErrInvalidComponent, what, s)
	}
	return nil
}

func allOf(s string, ok func(byte) bool) bool {
	for i := 0; i < len(s); i++ {
		if !ok(s[i]) {
			return false
		}
	}
	return true
}

func isLower(c byte) bool { return c >= 'a' && c <= 'z' }
func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// LegacyRawRoot is the top-level directory the raw archive had before ADR-089 and
// ADR-092 put the source first: <root>/raw/<source>/... then, <root>/<source>/raw/...
// now. LegacyCanonicalRoot is the same for canonical files; those were canonical-v1
// and are rebuilt, never moved.
const (
	LegacyRawRoot       = "raw"
	LegacyCanonicalRoot = "canonical"
)

// LegacyRawDir returns where a source's raw data was under the old layout
// (raw/<source>) and where it is now (<source>/raw), both relative to the data root.
// It reports ErrInvalidComponent for a source that cannot be a path component,
// which includes every name that would collide with the old top-level directories
// only by being spelled like a stage.
func LegacyRawDir(source string) (legacy, current string, err error) {
	if err := validComponent("source", source); err != nil {
		return "", "", err
	}
	if source == LegacyRawRoot || source == LegacyCanonicalRoot {
		return "", "", fmt.Errorf("%w: source %q is a stage name", ErrInvalidComponent, source)
	}
	current, err = StageDir(source, Raw)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(LegacyRawRoot, source), current, nil
}
