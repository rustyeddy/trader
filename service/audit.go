package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ErrAuditFailed reports that Audit finished and found the data not sound: a month
// that is stale, bad or missing (unless allowed), a gap the calendar does not
// explain (unless allowed), or a canonical file that disagrees with its raw. The
// AuditReport returned with it is complete.
var ErrAuditFailed = errors.New("service: audit failed")

// AuditRequest asks for the audit of every instrument's canonical data at one
// interval over [From, To). AllowBad and AllowMissing mean what they do for a
// load: they accept months with bad records, and months with no file or bars
// missing that the calendar does not explain. Sample, when positive, limits the
// raw cross-check to that many months per instrument (see VerifyRequest); the
// months and gaps are always checked over the whole range.
type AuditRequest struct {
	Instruments  []string
	Interval     string
	From, To     time.Time
	AllowBad     bool
	AllowMissing bool
	Sample       int
}

// AuditMonth is what one month's canonical file came to (see MonthStatus).
type AuditMonth struct {
	Year   int    `json:"year"`
	Month  int    `json:"month"`
	State  string `json:"state"`
	Path   string `json:"path"`
	Good   int    `json:"good"`
	Bad    int    `json:"bad"`
	Reason string `json:"reason,omitempty"`
}

// AuditGap is a run of missing bars inside the series (see Gap): "closure" for a
// year-end holiday, "known_hole" for a run on the reviewed list of bars the provider
// does not have, "unexplained" for anything else. Weekends are not reported.
type AuditGap struct {
	Kind  string    `json:"kind"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Bars  int       `json:"bars"`
}

// AuditFinding is one disagreement between a canonical file and its raw.
type AuditFinding struct {
	Kind   string     `json:"kind"`
	Time   *time.Time `json:"time,omitempty"`
	RawRow int        `json:"raw_row,omitempty"`
	Detail string     `json:"detail"`
}

// The raw states of an audit, those of a verification plus AuditUnverifiable.
const (
	// AuditUnverifiable: there is a canonical file for the month and no raw
	// partition to check it against. It fails the audit: it cannot be certified.
	AuditUnverifiable = "unverifiable"
)

// AuditPartition is the raw cross-check of one month (see PartitionVerification).
type AuditPartition struct {
	Year      int            `json:"year"`
	Month     int            `json:"month"`
	Path      string         `json:"path,omitempty"`
	State     string         `json:"state"`
	RawRows   int            `json:"raw_rows"`
	Bars      int            `json:"bars"`
	Bad       int            `json:"bad"`
	Explained map[string]int `json:"explained,omitempty"`
	Findings  []AuditFinding `json:"findings,omitempty"`
	Omitted   int            `json:"omitted_findings,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// InstrumentAudit is everything found for one instrument.
type InstrumentAudit struct {
	Instrument string       `json:"instrument"`
	Months     []AuditMonth `json:"months"`
	// Bars counts the bars of the loaded series; zero when the load was refused.
	Bars int        `json:"bars"`
	Gaps []AuditGap `json:"gaps"`
	// LoadError is why the series could not be loaded as asked, if it could not.
	LoadError string           `json:"load_error,omitempty"`
	Raw       []AuditPartition `json:"raw"`
	// OK is true when nothing about this instrument fails the audit.
	OK bool `json:"ok"`
}

// AuditSummary counts what the audit found, over every instrument.
type AuditSummary struct {
	Months          int `json:"months"`
	MonthsLoaded    int `json:"months_loaded"`
	MonthsBad       int `json:"months_bad"`
	MonthsMissing   int `json:"months_missing"`
	MonthsStale     int `json:"months_stale"`
	Bars            int `json:"bars"`
	GapsUnexplained int `json:"gaps_unexplained"`
	GapsClosure     int `json:"gaps_known_closure"`
	GapsKnownHole   int `json:"gaps_known_hole"`
	RawVerified     int `json:"raw_verified"`
	RawFailed       int `json:"raw_failed"`
	RawNoRaw        int `json:"raw_no_raw"`
	// RawUnverifiable counts months that have a canonical file but no raw to check
	// it against; each fails the audit.
	RawUnverifiable int `json:"raw_unverifiable"`
	// RawChecked counts the months cross-checked against raw (all of them, unless
	// the audit was sampled).
	RawChecked         int `json:"raw_checked"`
	RawNoCanonical     int `json:"raw_no_canonical"`
	RawFindings        int `json:"raw_findings"`
	InstrumentsFailing int `json:"instruments_failing"`
}

// AuditReport is the result of Audit: machine-readable as it is (it marshals to
// JSON), and what a transport formats for people.
type AuditReport struct {
	Source       string            `json:"source"`
	Interval     string            `json:"interval"`
	From         time.Time         `json:"from"`
	To           time.Time         `json:"to"`
	AllowBad     bool              `json:"allow_bad"`
	AllowMissing bool              `json:"allow_missing"`
	Sample       int               `json:"sample"`
	Instruments  []InstrumentAudit `json:"instruments"`
	Summary      AuditSummary      `json:"summary"`
	OK           bool              `json:"ok"`
}

// Audit is the operator's integrity check of one source's canonical data. For each
// instrument it combines what the other use cases look at one by one, so the
// operator has one answer:
//
//   - every month's state: loaded, bad, missing or stale (a file from another
//     build, whose raw fingerprint, builder, validator and availability policy are
//     the file's own manifest);
//   - the bars missing inside the series against the market calendar: weekends are
//     not reported, year-end holidays are known closures, anything else is
//     unexplained (see LoadSeries);
//   - each canonical file against the raw partition it was built from. A month
//     with a canonical file and no raw cannot be certified and fails the audit
//     ("unverifiable"); a month with neither is the missing month above. Raw-row
//     accounting, exact prices and volume, bad-record reasons, fingerprint, and
//     canonical validity such as duplicate or out-of-order times and invalid OHLC
//     (see VerifyCanonical).
//
// It reuses those rules and defines none of its own. A report is always returned
// with ErrAuditFailed when the data is not sound; a request that cannot run, or
// cancellation, returns the error alone. Nothing is written. log may be nil.
func Audit(ctx context.Context, log *slog.Logger, req AuditRequest) (AuditReport, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if len(req.Instruments) == 0 {
		return AuditReport{}, errors.New("audit: no instrument")
	}
	if !req.To.After(req.From) {
		return AuditReport{}, errors.New("audit: --to must be after --from")
	}
	if req.Sample < 0 {
		return AuditReport{}, errors.New("audit: --sample must not be negative")
	}
	report := AuditReport{Source: "oanda", Interval: req.Interval, From: req.From.UTC(), To: req.To.UTC(),
		AllowBad: req.AllowBad, AllowMissing: req.AllowMissing, Sample: req.Sample, OK: true}

	for _, symbol := range req.Instruments {
		if err := ctx.Err(); err != nil {
			return AuditReport{}, err
		}
		loaded, lerr := LoadSeries(ctx, log, LoadRequest{Instruments: []string{symbol}, Interval: req.Interval,
			From: req.From, To: req.To, AllowBad: req.AllowBad, AllowMissing: req.AllowMissing})
		if cerr := ctx.Err(); cerr != nil {
			return AuditReport{}, cerr
		}
		if len(loaded) == 0 { // the request itself was refused
			return AuditReport{}, lerr
		}
		ia := InstrumentAudit{Instrument: symbol, Months: []AuditMonth{}, Gaps: []AuditGap{}, Raw: []AuditPartition{}, OK: true}
		r := loaded[0]
		for _, m := range r.Months {
			ia.Months = append(ia.Months, AuditMonth{Year: m.Year, Month: int(m.Month), State: string(m.State), Path: m.Path,
				Good: m.Good, Bad: m.Bad, Reason: m.Reason})
			report.Summary.Months++
			switch m.State {
			case MonthLoaded:
				report.Summary.MonthsLoaded++
			case MonthBad:
				report.Summary.MonthsBad++
			case MonthMissing:
				report.Summary.MonthsMissing++
			case MonthStale:
				report.Summary.MonthsStale++
			}
		}
		for _, g := range r.Gaps {
			ia.Gaps = append(ia.Gaps, AuditGap{Kind: string(g.Kind), Start: g.Start, End: g.End, Bars: g.Bars})
			switch g.Kind {
			case GapUnexplained:
				report.Summary.GapsUnexplained++
			case GapKnownHole:
				report.Summary.GapsKnownHole++
			default:
				report.Summary.GapsClosure++
			}
		}
		if r.Err != nil {
			ia.LoadError, ia.OK = r.Err.Error(), false
		} else {
			ia.Bars = len(r.Series.Bars)
			report.Summary.Bars += ia.Bars
		}

		hasCanonical := map[[2]int]bool{}
		for _, m := range r.Months {
			hasCanonical[[2]int{m.Year, int(m.Month)}] = m.State != MonthMissing
		}
		verr := VerifyCanonical(ctx, log, VerifyRequest{Instruments: []string{symbol}, Interval: req.Interval,
			From: req.From, To: req.To, Sample: req.Sample}, func(p PartitionVerification) {
			ap := auditPartition(p)
			report.Summary.RawChecked++
			report.Summary.RawFindings += len(p.Findings) + p.Omitted
			if p.State == VerifyNoRaw && hasCanonical[[2]int{p.Year, int(p.Month)}] {
				ap.State = AuditUnverifiable
				report.Summary.RawUnverifiable++
				ia.OK = false
			}
			ia.Raw = append(ia.Raw, ap)
			switch p.State {
			case VerifyOK:
				report.Summary.RawVerified++
			case VerifyBad:
				report.Summary.RawFailed++
				ia.OK = false
			case VerifyNoRaw:
				if ap.State != AuditUnverifiable {
					report.Summary.RawNoRaw++
				}
			case VerifyNoCanonical:
				report.Summary.RawNoCanonical++
			}
		})
		if verr != nil && !errors.Is(verr, ErrVerifyFailed) {
			return AuditReport{}, verr
		}
		if !ia.OK {
			report.OK = false
			report.Summary.InstrumentsFailing++
		}
		report.Instruments = append(report.Instruments, ia)
	}
	if !report.OK {
		return report, fmt.Errorf("%w: %d of %d instruments", ErrAuditFailed, report.Summary.InstrumentsFailing, len(report.Instruments))
	}
	return report, nil
}

func auditPartition(p PartitionVerification) AuditPartition {
	ap := AuditPartition{Year: p.Year, Month: int(p.Month), Path: p.Path, State: p.State, RawRows: p.RawRows,
		Bars: p.Bars, Bad: p.Bad, Explained: p.Explained, Omitted: p.Omitted}
	if p.Err != nil {
		ap.Error = p.Err.Error()
	}
	for _, f := range p.Findings {
		af := AuditFinding{Kind: string(f.Kind), RawRow: f.RawRow, Detail: f.Detail}
		if !f.Time.IsZero() {
			t := f.Time.UTC()
			af.Time = &t
		}
		ap.Findings = append(ap.Findings, af)
	}
	return ap
}
