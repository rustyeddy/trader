#!/usr/bin/env python3
"""
validate-candles.py — independent oracle for the trader canonical candle store.

Shares NO code, logic, or language with the trader Go codebase. Verifies that
canonical candle files faithfully represent the raw preservation tree under the
conventions each canonical file CLAIMS in its own header. Uses Python's zoneinfo
(independent IANA tzdata implementation from Go's time package) for all DST math,
so a DST bug in trader cannot be silently mirrored here.

Ground truth model:
  raw tree       = what OANDA actually said (bid/ask, true ISO timestamps)
  canonical file = the claim under test (header declares schema + alignment)
  (optional) TradingView export = external oracle independent of both trees

Checks:
  A. Row fidelity      every valid canonical candle has a raw row at the same
                       timestamp with exactly equal bid OHLC (x100000), and
                       every complete raw row has a canonical counterpart.
  B. Grid conformance  every canonical timestamp lands on the grid independently
                       computed from the header's claimed align/align_tz —
                       including across DST transitions (H4/D1 phase-shift).
  C. Month membership  every timestamp falls inside the file's claimed
                       calendar month (UTC).
  D. Gap hygiene       invalid rows (flags bit 0x1 clear) have all-zero OHLC
                       and a real, on-grid slot timestamp — every slot always
                       carries its own authoritative timestamp, valid or not
                       (required for correct DST-transition reconstruction;
                       a zero timestamp or a nonzero OHLC on a gap row is a
                       violation).
  E. Spacing sanity    consecutive valid candles are >= 1 step apart and their
                       count does not exceed the month's slot capacity.
  F. (optional) TV     bid-close cross-check against a TradingView CSV export
                       (--tv FILE), tolerance configurable.

Schemas understood (from on-disk documentation only, parsed fresh):

  raw-v1 (raw tree):
    # schema=raw-v1 source=... instrument=... tf=... year=... month=...
    time,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,volume,complete
    ISO8601 UTC timestamps, plain decimals.

  candle-v2 (canonical, post-regen):
    # schema=candle-v2 source=... instrument=... tf=... year=... month=...
    #   align=17:00 align_tz=America/New_York
    timestamp,open,high,low,close,avgspread,maxspread,ticks,flags
    Unix epoch seconds, prices fixed-point x100000, flags bit 0x1 = valid.

  candle-v1 (legacy canonical, pre-regen — accepted with --legacy so the tool
  can demonstrate the OLD store failing its checks):
    no header; timestamp,high,open,low,close,avgspread,maxspread,ticks,flags

Exit codes: 0 = all checks passed, 1 = violations found, 2 = usage/IO error.

Usage:
  ./validate-candles.py --raw RAW.csv --canonical CANON.csv
  ./validate-candles.py --raw RAW.csv --canonical CANON.csv --tv TV.csv
  ./validate-candles.py --raw RAW.csv --canonical CANON.csv --legacy
  ./validate-candles.py --raw RAW.csv --canonical CANON.csv --max-report 50

Batch (directory) mode — pass tree roots instead of files and every raw-v1
file under --raw is paired with its counterpart at the same relative path
under --canonical (mirroring the raw/candles tree layout: <root>/<oanda-or-
other-source>/<INSTRUMENT>/<YYYY>/<MM>/<INSTRUMENT>-<YYYY>-<MM>-<tf>.csv):

  ./validate-candles.py --raw data/raw/oanda --canonical data/candles/oanda
  ./validate-candles.py --raw data/raw/oanda --canonical data/candles/oanda \\
      --instruments EURUSD,GBPUSD --timeframe m1,h1 --from 2026-01 --to 2026-07
  ./validate-candles.py --raw data/raw/oanda --canonical data/candles/oanda --quiet

A missing or unreadable canonical/raw file for a given month is reported as
an [ERROR] line and counted as a failure; it does not abort the batch.
--tv (TradingView cross-check) is single-file mode only.
"""
import argparse
import csv
import os
import re
import sys
from datetime import datetime, timedelta, timezone

try:
    from zoneinfo import ZoneInfo
except ImportError:  # pragma: no cover
    print("error: Python 3.9+ with zoneinfo required", file=sys.stderr)
    sys.exit(2)

PRICE_SCALE = 100_000

TF_SECONDS = {"m1": 60, "h1": 3600, "h4": 14400, "d1": 86400, "w1": 604800}

# Granularities that follow the provider's daily alignment anchor (per OANDA
# docs, alignment applies to H2 and above; below that the grid is plain UTC).
DAILY_ALIGNED = {"h4", "d1", "w1"}

# <INSTRUMENT>-<YYYY>-<MM>-<tf>.csv, e.g. EURUSD-2026-06-m1.csv
FILENAME_RE = re.compile(
    r"^(?P<inst>[A-Za-z0-9]+)-(?P<year>\d{4})-(?P<month>\d{2})-(?P<tf>[A-Za-z0-9]+)\.csv$"
)


class FileError(Exception):
    """A single file/pair is malformed or violates a documented convention.

    Recoverable in batch mode (skip and report that one pair); fatal in
    single-file mode (print and exit 2), same as the old fail()-then-exit
    behavior this replaces for per-file problems.
    """


def fail(msg):
    """Fatal CLI-usage error (bad arguments) — always exits immediately."""
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(2)


def parse_header_comment(lines):
    """Parse '# key=value key=value' comment lines into a dict."""
    meta = {}
    for ln in lines:
        body = ln.lstrip()
        if not body.startswith("#"):
            continue
        for tok in body[1:].split():
            if "=" in tok:
                k, v = tok.split("=", 1)
                meta[k.strip().lower()] = v.strip()
    return meta


def read_raw(path):
    """Returns (meta, {ts_epoch: (bo,bh,bl,bc, complete)}) with prices scaled x100000."""
    with open(path, newline="") as f:
        all_lines = f.readlines()
    meta = parse_header_comment(all_lines)
    if meta.get("schema") != "raw-v1":
        raise FileError(f"{path}: expected '# schema=raw-v1' header, got {meta.get('schema')!r}")
    data_lines = [ln for ln in all_lines if not ln.lstrip().startswith("#")]
    reader = csv.reader(data_lines)
    rows = {}
    header_seen = False
    for rec in reader:
        if not rec:
            continue
        if not header_seen:
            header_seen = True
            hdr = [c.strip().lower() for c in rec]
            if hdr[:2] != ["time", "bid_o"]:
                raise FileError(f"{path}: unexpected raw-v1 column header: {hdr}")
            continue
        if len(rec) < 11:
            print(f"warning: raw row with {len(rec)} fields skipped: {rec}", file=sys.stderr)
            continue
        ts = int(datetime.strptime(rec[0].strip(), "%Y-%m-%dT%H:%M:%SZ")
                 .replace(tzinfo=timezone.utc).timestamp())
        # Scale via round() to avoid float representation drift (2.01655 -> 201655).
        bo, bh, bl, bc = (round(float(x) * PRICE_SCALE) for x in rec[1:5])
        complete = rec[10].strip().lower() == "true"
        if ts in rows:
            print(f"warning: duplicate raw timestamp {rec[0]}", file=sys.stderr)
        rows[ts] = (bo, bh, bl, bc, complete)
    return meta, rows


def read_canonical(path, legacy=False):
    """Returns (meta, [(ts,o,h,l,c,valid), ...] in file order) prices x100000."""
    with open(path, newline="") as f:
        all_lines = f.readlines()
    meta = parse_header_comment(all_lines)
    if not legacy and meta.get("schema") != "candle-v2":
        raise FileError(f"{path}: expected '# schema=candle-v2' header (use --legacy for old files), "
                         f"got {meta.get('schema')!r}")
    data_lines = [ln for ln in all_lines if not ln.lstrip().startswith("#")]
    reader = csv.reader(data_lines)
    out = []
    for rec in reader:
        if not rec:
            continue
        if rec[0].strip().lower() in ("timestamp", "time"):
            continue
        if len(rec) < 9:
            print(f"warning: canonical row with {len(rec)} fields skipped: {rec}", file=sys.stderr)
            continue
        ts = int(rec[0])
        if legacy:
            h, o, l, c = (int(x) for x in rec[1:5])   # v1 order: H,O,L,C
        else:
            o, h, l, c = (int(x) for x in rec[1:5])   # v2 order: O,H,L,C
        valid = bool(int(rec[8], 0) & 0x1)
        out.append((ts, o, h, l, c, valid))
    return meta, out


def month_bounds_utc(year, month):
    start = datetime(year, month, 1, tzinfo=timezone.utc)
    end = datetime(year + (month == 12), (month % 12) + 1, 1, tzinfo=timezone.utc)
    return int(start.timestamp()), int(end.timestamp())


def expected_grid(year, month, tf, align_hhmm, align_tz):
    """Independently compute the set of valid candle-open epochs for the month.

    For plain-UTC granularities (m1, h1) the grid is simply every tf boundary
    from epoch. Daily-aligned granularities open at fixed LOCAL wall-clock
    times in align_tz (DST-aware via zoneinfo): d1 at align_hhmm; h4 at
    align_hhmm plus every 4 local hours (17:00 align -> 1/5/9/13/17/21:00
    local). The UTC phase therefore switches at the DST transition instant
    (2am local), mid-session — NOT at the next session open. Verified against
    raw OANDA archive rows on 2005-2012 transition days (issue #182): the
    1:00-5:00 local slot spans 3 real hours on spring-forward days and 5 on
    fall-back days. On fall-back days the repeated 1:00 local hour opens only
    one candle (the first UTC occurrence).

    Returns a set of epoch seconds covering [month_start, month_end) plus one
    day of margin each side (so boundary candles are judged by membership
    checks, not by grid-set truncation).
    """
    step = TF_SECONDS[tf]
    mstart, mend = month_bounds_utc(year, month)
    lo, hi = mstart - 86400, mend + 86400
    if tf not in DAILY_ALIGNED:
        first = (lo // step) * step
        return set(range(first, hi + step, step))
    hh, mm = (int(x) for x in align_hhmm.split(":"))
    tz = ZoneInfo(align_tz)
    if tf == "d1":
        local_hours = {hh}
    else:  # h4 (and w1 treated as h4-style subdivision is not stored; keep h4)
        local_hours = {(hh + 4 * k) % 24 for k in range(6)}
    grid = set()
    prev = None
    # Walk UTC hour-by-hour; a slot opens wherever the local wall clock reads
    # one of local_hours exactly. Candidates under 2h after the previous kept
    # slot are the fall-back repeated hour — skip them (legitimate spacing is
    # never below 3h).
    t = (lo // 3600) * 3600
    while t <= hi:
        local = datetime.fromtimestamp(t, tz)
        if local.minute == mm and local.hour in local_hours:
            if prev is None or t - prev >= 7200:
                grid.add(t)
                prev = t
        t += 3600
    return grid


def iso(ts):
    return datetime.fromtimestamp(ts, tz=timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def read_tv(path):
    """TradingView export: expects 'time' + open/high/low/close columns.
    time may be unix seconds or ISO8601. Returns {epoch: close_x100000}."""
    out = {}
    with open(path, newline="") as f:
        reader = csv.DictReader(f)
        cols = {c.lower().strip(): c for c in reader.fieldnames or []}
        tcol = cols.get("time") or cols.get("timestamp") or cols.get("date")
        ccol = cols.get("close")
        if not tcol or not ccol:
            raise FileError(f"{path}: need 'time' and 'close' columns; found {list(cols)}")
        for row in reader:
            traw = row[tcol].strip()
            if traw.isdigit():
                ts = int(traw)
            else:
                traw = traw.replace("Z", "+00:00")
                ts = int(datetime.fromisoformat(traw).timestamp())
            out[ts] = round(float(row[ccol]) * PRICE_SCALE)
    return out


CHECK_NAMES = {"A": "row fidelity vs raw", "B": "grid conformance (DST-aware)",
               "C": "month membership", "D": "gap hygiene",
               "E": "spacing sanity", "F": "TradingView cross-check"}


class ValidationResult:
    """Everything print_full_report/run_batch need for one raw/canonical pair."""

    def __init__(self, instrument, tf, year, month, align, align_tz, legacy,
                 raw_count, valid_count, gap_count, violations):
        self.instrument = instrument
        self.tf = tf
        self.year = year
        self.month = month
        self.align = align
        self.align_tz = align_tz
        self.legacy = legacy
        self.raw_count = raw_count
        self.valid_count = valid_count
        self.gap_count = gap_count
        self.violations = violations  # dict: "A".."F" -> [str, ...]

    def total_violations(self):
        return sum(len(v) for v in self.violations.values())


def validate_pair(raw_path, canonical_path, args):
    """Run checks A-F for one raw/canonical file pair.

    Raises FileError (bad schema/convention) or OSError (unreadable file) —
    both are recoverable per-pair in batch mode and fatal in single-file mode.
    """
    raw_meta, raw = read_raw(raw_path)
    can_meta, canon = read_canonical(canonical_path, legacy=args.legacy)

    # Conventions under test come from the canonical file's own claims,
    # falling back to raw header + explicit flags for legacy files.
    tf = (can_meta.get("tf") or raw_meta.get("tf") or "").lower()
    if tf not in TF_SECONDS:
        raise FileError(f"unknown/missing tf {tf!r} in headers")
    year = int(can_meta.get("year") or raw_meta.get("year"))
    month = int(can_meta.get("month") or raw_meta.get("month"))
    align = args.align or can_meta.get("align") or "17:00"
    align_tz = args.align_tz or can_meta.get("align_tz") or "America/New_York"
    inst_r, inst_c = raw_meta.get("instrument"), can_meta.get("instrument")
    if inst_r and inst_c and inst_r != inst_c:
        raise FileError(f"instrument mismatch: raw={inst_r} canonical={inst_c}")

    step = TF_SECONDS[tf]
    mstart, mend = month_bounds_utc(year, month)
    grid = expected_grid(year, month, tf, align, align_tz)

    violations = {k: [] for k in "ABCDEF"}
    valid_rows = [(ts, o, h, l, c) for ts, o, h, l, c, v in canon if v]
    invalid_rows = [(ts, o, h, l, c) for ts, o, h, l, c, v in canon if not v]

    # --- A. Row fidelity (both directions) ---
    for ts, o, h, l, c in valid_rows:
        rr = raw.get(ts)
        if rr is None:
            violations["A"].append(f"canonical {iso(ts)} has NO raw row at that timestamp")
            continue
        bo, bh, bl, bc, _complete = rr
        if (o, h, l, c) != (bo, bh, bl, bc):
            violations["A"].append(
                f"OHLC mismatch at {iso(ts)}: canonical=({o},{h},{l},{c}) raw_bid=({bo},{bh},{bl},{bc})")
    canon_ts = {ts for ts, *_ in valid_rows}
    for ts, (bo, bh, bl, bc, complete) in sorted(raw.items()):
        if complete and mstart <= ts < mend and ts not in canon_ts:
            violations["A"].append(f"raw complete row {iso(ts)} missing from canonical")

    # --- B. Grid conformance (independent zoneinfo DST math) ---
    for ts, *_ in valid_rows:
        if ts not in grid:
            violations["B"].append(
                f"{iso(ts)} not on claimed grid (align={align} {align_tz}, tf={tf})")

    # --- C. Month membership ---
    for ts, *_ in valid_rows:
        if not (mstart <= ts < mend):
            violations["C"].append(f"{iso(ts)} outside claimed month {year}-{month:02d}")

    # --- D. Gap hygiene ---
    # Every slot — valid or not — carries its own authoritative grid
    # timestamp (required for correct DST-transition reconstruction; see
    # the candle-v2 schema note above), so an invalid/gap row is well-formed
    # iff its OHLC is all-zero AND its timestamp is real and on-grid, not
    # iff the whole row is zero.
    for ts, o, h, l, c in invalid_rows:
        if (o, h, l, c) != (0, 0, 0, 0):
            violations["D"].append(
                f"invalid row at {iso(ts) if ts else 'ts=0'} has nonzero OHLC: ({o},{h},{l},{c})")
        if ts == 0:
            violations["D"].append("invalid row missing its slot timestamp (ts=0)")
        elif ts not in grid:
            violations["D"].append(
                f"invalid row {iso(ts)} timestamp not on claimed grid (align={align} {align_tz}, tf={tf})")

    # --- E. Spacing sanity ---
    ordered = sorted(ts for ts, *_ in valid_rows)
    for a, b in zip(ordered, ordered[1:]):
        # Sub-step spacing is legal iff both candles sit on the independently
        # computed grid: DST-transition sessions are 23h/25h, producing one
        # legitimately short (or long) block per transition. The grid itself
        # encodes which spacings are real; anything off-grid is caught by B.
        if b - a < step and not (a in grid and b in grid):
            violations["E"].append(f"candles {iso(a)} and {iso(b)} closer than one {tf} step")
    capacity = (mend - mstart) // step + 2  # +margin for aligned grids
    if len(ordered) > capacity:
        violations["E"].append(f"{len(ordered)} valid candles exceeds month capacity ~{capacity}")

    # --- F. Optional TradingView cross-check ---
    if args.tv:
        tv = read_tv(args.tv)
        matched = 0
        for ts, o, h, l, c in valid_rows:
            if ts in tv:
                matched += 1
                if abs(c - tv[ts]) > args.tv_tolerance:
                    violations["F"].append(
                        f"close mismatch vs TV at {iso(ts)}: canonical={c} tv={tv[ts]}")
        if matched == 0:
            violations["F"].append(
                "no timestamp overlap with TV export — likely a grid/timezone mismatch "
                "(this itself is a finding)")

    return ValidationResult(
        instrument=can_meta.get("instrument") or inst_r or "?",
        tf=tf, year=year, month=month, align=align, align_tz=align_tz, legacy=args.legacy,
        raw_count=len(raw), valid_count=len(valid_rows), gap_count=len(invalid_rows),
        violations=violations,
    )


def print_full_report(result, args):
    """Verbose single-file report (unchanged format from before batch mode existed)."""
    total = 0
    print(f"validate-candles: {result.instrument} {result.tf} "
          f"{result.year}-{result.month:02d}  (align={result.align} {result.align_tz}, "
          f"{'legacy-v1' if result.legacy else 'candle-v2'})")
    print(f"  raw rows: {result.raw_count}   canonical: {result.valid_count} valid / {result.gap_count} gap")
    for k in "ABCDEF":
        if k == "F" and not args.tv:
            continue
        v = result.violations[k]
        total += len(v)
        status = "OK " if not v else "FAIL"
        print(f"  [{status}] {k}: {CHECK_NAMES[k]}" + (f" — {len(v)} violation(s)" if v else ""))
        for line in v[:args.max_report]:
            print(f"         {line}")
        if len(v) > args.max_report:
            print(f"         ... and {len(v) - args.max_report} more")
    if total:
        print(f"RESULT: FAIL ({total} violation(s))")
    else:
        print("RESULT: PASS")
    return total


def parse_year_month(s):
    y, m = s.split("-", 1)
    return int(y), int(m)


def find_pairs(raw_root, canonical_root, instruments=None, timeframes=None, from_ym=None, to_ym=None):
    """Walk raw_root for raw-v1 candle CSVs and pair each with its counterpart
    at the same relative path under canonical_root. Returns a sorted list of
    (raw_path, canonical_path, instrument, tf, year, month) tuples; existence
    of the canonical file is NOT checked here (a missing counterpart becomes
    a reported [ERROR] entry in run_batch, not a batch-construction failure).
    """
    pairs = []
    for dirpath, _dirnames, filenames in os.walk(raw_root):
        for fn in filenames:
            m = FILENAME_RE.match(fn)
            if not m:
                continue
            tf = m.group("tf").lower()
            if tf not in TF_SECONDS:
                continue
            inst = m.group("inst").upper()
            year = int(m.group("year"))
            month = int(m.group("month"))
            if instruments and inst not in instruments:
                continue
            if timeframes and tf not in timeframes:
                continue
            ym = (year, month)
            if from_ym and ym < from_ym:
                continue
            if to_ym and ym > to_ym:
                continue
            raw_path = os.path.join(dirpath, fn)
            rel = os.path.relpath(raw_path, raw_root)
            canonical_path = os.path.join(canonical_root, rel)
            pairs.append((raw_path, canonical_path, inst, tf, year, month))
    pairs.sort(key=lambda p: (p[2], p[3], p[4], p[5]))
    return pairs


def run_batch(pairs, args):
    """Validate every pair, print a per-file line (PASS lines suppressed
    under --quiet), and exit 1 if anything failed or errored, else 0.
    """
    passed = failed = errored = 0
    total_violations = 0

    for raw_path, canonical_path, inst, tf, year, month in pairs:
        label = f"{inst} {tf} {year}-{month:02d}"
        try:
            result = validate_pair(raw_path, canonical_path, args)
        except FileNotFoundError:
            errored += 1
            print(f"[ERROR] {label}: canonical file not found ({canonical_path})")
            continue
        except (FileError, OSError, ValueError) as e:
            errored += 1
            print(f"[ERROR] {label}: {e}")
            continue

        total = result.total_violations()
        total_violations += total
        if total == 0:
            passed += 1
            if not args.quiet:
                print(f"[PASS]  {label}  ({result.raw_count} raw, {result.valid_count} valid)")
        else:
            failed += 1
            print(f"[FAIL]  {label} — {total} violation(s)")
            if not args.quiet:
                for k in "ABCDE":
                    v = result.violations[k]
                    if not v:
                        continue
                    print(f"    {k}: {CHECK_NAMES[k]} — {len(v)} violation(s)")
                    for line in v[:args.max_report]:
                        print(f"         {line}")
                    if len(v) > args.max_report:
                        print(f"         ... and {len(v) - args.max_report} more")

    print()
    summary = f"batch: {len(pairs)} file pair(s) — {passed} pass, {failed} fail, {errored} error"
    if total_violations:
        summary += f", {total_violations} violation(s) total"
    print(summary)
    sys.exit(1 if (failed or errored) else 0)


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--raw", required=True,
                    help="raw-v1 monthly file, or a raw tree root for batch mode")
    ap.add_argument("--canonical", required=True,
                    help="canonical monthly file, or a canonical tree root for batch mode")
    ap.add_argument("--tv", help="optional TradingView export for external cross-check (single-file mode only)")
    ap.add_argument("--legacy", action="store_true",
                    help="canonical file(s) are old v1 (no header, H,O,L,C order)")
    ap.add_argument("--align", default=None, help="override claimed align (HH:MM)")
    ap.add_argument("--align-tz", default=None, help="override claimed align_tz")
    ap.add_argument("--tv-tolerance", type=int, default=0,
                    help="allowed |close| diff vs TV in x100000 units (default exact)")
    ap.add_argument("--max-report", type=int, default=20,
                    help="max violations printed per check")
    ap.add_argument("--instruments", default=None,
                    help="batch mode: comma-separated instrument filter, e.g. EURUSD,GBPUSD")
    ap.add_argument("--timeframe", default=None,
                    help="batch mode: comma-separated timeframe filter, e.g. m1,h1")
    ap.add_argument("--from", dest="from_month", default=None,
                    help="batch mode: start month inclusive YYYY-MM")
    ap.add_argument("--to", dest="to_month", default=None,
                    help="batch mode: end month inclusive YYYY-MM")
    ap.add_argument("--quiet", action="store_true",
                    help="batch mode: suppress PASS lines and per-check detail; still shows FAIL/ERROR")
    args = ap.parse_args()

    def kind(p):
        if os.path.isdir(p):
            return "dir"
        if os.path.isfile(p):
            return "file"
        return "missing"

    raw_kind, canonical_kind = kind(args.raw), kind(args.canonical)
    if raw_kind == "missing" or canonical_kind == "missing":
        fail(f"path does not exist: {args.raw if raw_kind == 'missing' else args.canonical}")
    raw_is_dir = raw_kind == "dir"
    canonical_is_dir = canonical_kind == "dir"
    if raw_is_dir != canonical_is_dir:
        fail("--raw and --canonical must both be files or both be directories "
             f"(got raw={raw_kind}, canonical={canonical_kind})")

    if raw_is_dir:
        if args.tv:
            fail("--tv is not supported in batch (directory) mode")
        instruments = {s.strip().upper() for s in args.instruments.split(",")} if args.instruments else None
        timeframes = {s.strip().lower() for s in args.timeframe.split(",")} if args.timeframe else None
        from_ym = parse_year_month(args.from_month) if args.from_month else None
        to_ym = parse_year_month(args.to_month) if args.to_month else None
        pairs = find_pairs(args.raw, args.canonical, instruments, timeframes, from_ym, to_ym)
        if not pairs:
            fail("no raw-v1 candle files found under --raw matching the given filters")
        run_batch(pairs, args)
        return

    try:
        result = validate_pair(args.raw, args.canonical, args)
    except (FileError, OSError, ValueError) as e:
        fail(str(e))
        return  # unreachable; fail() exits
    total = print_full_report(result, args)
    sys.exit(1 if total else 0)


if __name__ == "__main__":
    main()
