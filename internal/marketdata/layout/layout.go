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
	Raw       Stage = "raw"
	Canonical Stage = "canonical"
)

// Valid reports whether s is exactly Raw or Canonical.
func (s Stage) Valid() bool { return s == Raw || s == Canonical }

// Errors, classifiable with errors.Is.
var (
	// ErrInvalidComponent reports an identifier that cannot be a path component.
	ErrInvalidComponent = errors.New("layout: invalid path component")
	// ErrInvalidRoot reports an empty data root.
	ErrInvalidRoot = errors.New("layout: invalid data root")
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
		return fmt.Errorf("%w: stage %q is not raw or canonical", ErrInvalidComponent, p.Stage)
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

// Dir returns the partition's directory under root.
func (p Partition) Dir(root string) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	base, err := StageDir(root, p.Source, p.Stage)
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

// Path returns the partition file's path under root.
func (p Partition) Path(root string) (string, error) {
	dir, err := p.Dir(root)
	if err != nil {
		return "", err
	}
	name, err := p.FileName()
	if err != nil {
		return "", err
	}
	return underRoot(root, filepath.Join(dir, name))
}

// StageDir returns the directory that holds every instrument of one source and
// stage: <root>/<source>/<stage>.
func StageDir(root, source string, stage Stage) (string, error) {
	if root == "" {
		return "", ErrInvalidRoot
	}
	if err := validComponent("source", source); err != nil {
		return "", err
	}
	if !stage.Valid() {
		return "", fmt.Errorf("%w: stage %q is not raw or canonical", ErrInvalidComponent, stage)
	}
	return underRoot(root, filepath.Join(root, source, string(stage)))
}

// Parse is the inverse of Path: it returns the Partition a file under root
// names, or ErrNotPartitionPath when the path is not exactly
// <source>/<stage>/<instrument>/<YYYY>/<MM>/<file> with a file name that agrees
// with the directories.
func Parse(root, path string) (Partition, error) {
	if root == "" {
		return Partition{}, ErrInvalidRoot
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Partition{}, fmt.Errorf("%w: %s is not under the data root", ErrNotPartitionPath, path)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 6 {
		return Partition{}, fmt.Errorf("%w: %s has %d components under the root, want 6", ErrNotPartitionPath, path, len(parts))
	}
	year, yerr := strconv.Atoi(parts[3])
	month, merr := strconv.Atoi(parts[4])
	if yerr != nil || merr != nil || len(parts[3]) != 4 || len(parts[4]) != 2 {
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

// underRoot cleans path and confirms it is inside root: defense in depth, since
// validated components cannot escape.
func underRoot(root, path string) (string, error) {
	path = filepath.Clean(path)
	rel, err := filepath.Rel(filepath.Clean(root), path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %s escapes the data root", ErrInvalidComponent, path)
	}
	return path, nil
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
