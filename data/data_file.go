package data

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ulikunitz/xz/lzma"
)

type datafile struct {
	symbol string
	time.Time
	err error

	basedir string
	bytes   int64
	modtime time.Time
}

const tickPathLen = 5

func newDatafile(base string, sym string, t time.Time) *datafile {
	ds := &datafile{
		symbol:  sym,
		Time:    t,
		basedir: base,
	}
	return ds
}

func (d *datafile) URL() string {
	return fmt.Sprintf(
		"https://datafeed.dukascopy.com/datafeed/%s/%04d/%02d/%02d/%02dh_ticks.bi5",
		d.symbol,
		d.Time.Year(),
		d.Time.Month()-1,
		d.Time.Day(),
		d.Time.Hour())
}

func (d *datafile) Path() string {
	return filepath.Join(
		d.basedir,
		d.symbol,
		fmt.Sprintf("%04d", d.Time.Year()),
		fmt.Sprintf("%02d", d.Time.Month()),
		fmt.Sprintf("%02d", d.Time.Day()),
		fmt.Sprintf("%02dh_ticks.bi5", d.Time.Hour()),
	)
}

func (d *datafile) PathBin() string {
	return filepath.Join(d.basedir, fmt.Sprintf(
		"%s/%04d/%02d/%02d/%02dh_ticks.bin",
		d.symbol, d.Time.Year(), d.Time.Month(), d.Time.Day(), d.Time.Hour(),
	))
}

func (d *datafile) parsePath(path string) (err error) {
	parts := strings.Split(path, "/")
	nparts := len(parts)
	if nparts < tickPathLen {
		return fmt.Errorf("path not long enough %s", path)
	}
	if filepath.Ext(path) != ".bi5" {
		return fmt.Errorf("error expecting file extension (.bi5) got (%s)", path)
	}
	d.symbol = parts[nparts-5]
	d.basedir = filepath.Join(parts[:nparts-5]...)
	return nil
}

func (d *datafile) Exists() bool {
	p := d.Path()
	info, err := os.Stat(p)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil || info.IsDir() {
		return false
	}
	d.bytes = info.Size()
	d.modtime = info.ModTime()

	// Also check if it's a directory, as Stat returns info for both
	return err == nil && !info.IsDir()
}

// fileIsValid ensures that the file actually exists and is either
// a empty Weekend file or it is a complete non-corrupt lzh compressed
// dukas binary file format.
func (d *datafile) IsValid() bool {
	valid := true

	// 1. verify file exists

	// 2. if file is 0 sized makesure it is during the closing hours
	// over the weekend

	// 3. Decompress the file to ensure it is not corrupt

	// 4. Validate the file against the filename timestamp

	return valid
}

// download will first check to see if this particular tick data has
// already been downloaded from Dukascopy, if so just return.  If not
// it will return.
func (d *datafile) download(ctx context.Context, client *http.Client) error {

	// Skip if present.
	// TODO before the file is written we need to make sure it is a valid file.
	if d.Exists() && d.IsValid() {
		return nil
	}

	// Correctness-first timeout
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	fmt.Printf("Download %s %d-%02d-%02d:%02d... ",
		d.symbol,
		d.Time.Year(),
		d.Time.Month()-1,
		d.Time.Day(),
		d.Time.Hour())

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, d.URL(), nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	// resp, err := http.DefaultClient.Do(req)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", d.URL(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: http %d", d.URL(), resp.StatusCode)
	}

	dst := d.Path()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}

	tmp := dst + ".part"

	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}

	n, copyErr := io.Copy(f, resp.Body)
	// Important: flush + close BEFORE rename/stat
	syncErr := f.Sync() // optional but helpful for “why is size 0?”
	closeErr := f.Close()

	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if copyErr != nil {
			return fmt.Errorf("write %s: wrote %d bytes: %w", tmp, n, copyErr)
		}
		if syncErr != nil {
			return fmt.Errorf("sync %s: wrote %d bytes: %w", tmp, n, syncErr)
		}
		return fmt.Errorf("close %s: wrote %d bytes: %w", tmp, n, closeErr)
	}

	// Atomic move into place
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, dst, err)
	}

	// Trust the filesystem for bytes/modtime
	info, err := os.Stat(dst)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dst, err)
	}

	d.bytes = info.Size()
	d.modtime = info.ModTime()

	fmt.Printf("%6d bytes\n", n)

	// Optional: sanity-check bytes against what we copied
	if d.bytes != n || n == 0 {
		// fmt.Printf("Failed to download: %s\n", d.URL())
	}

	return nil
}

type Tick struct {
	TsUnixMS int64
	Ask      int32
	Bid      int32
	AskVol   float32
	BidVol   float32
}

var rePath = regexp.MustCompile(`[/\\](\d{4})[/\\](\d{2})[/\\](\d{2})[/\\](\d{2})h_ticks\.bi5$`)

func (d *datafile) baseHourUnixMS() (int64, error) {
	p := d.Path()
	m := rePath.FindStringSubmatch(p)
	if m == nil {
		return 0, fmt.Errorf("cannot parse datetime from path: %s", p)
	}
	year, _ := strconv.Atoi(m[1])
	mon, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	hh, _ := strconv.Atoi(m[4])

	t := time.Date(year, time.Month(mon), day, hh, 0, 0, 0, time.UTC)
	return t.UnixMilli(), nil
}

// ForEachTick decompresses BI5 and streams decoded ticks to fn.
// It does not write decompressed data to disk.
func (d *datafile) ForEachTick(ctx context.Context, fn func(Tick) error) error {
	path := d.Path()

	baseUnixMS, err := d.baseHourUnixMS()
	if err != nil {
		return err
	}

	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	zr, err := lzma.NewReader(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		return fmt.Errorf("lzma reader %s: %w", path, err)
	}

	const recSize = 20
	buf := make([]byte, recSize)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, err := io.ReadFull(zr, buf)
		if err == io.EOF {
			return nil
		}
		if err == io.ErrUnexpectedEOF {
			return fmt.Errorf("truncated tick record in %s", path)
		}
		if err != nil {
			return fmt.Errorf("read tick record %s: %w", path, err)
		}

		msOffset := binary.BigEndian.Uint32(buf[0:4])
		askU := binary.BigEndian.Uint32(buf[4:8])
		bidU := binary.BigEndian.Uint32(buf[8:12])

		askVol := math.Float32frombits(binary.BigEndian.Uint32(buf[12:16]))
		bidVol := math.Float32frombits(binary.BigEndian.Uint32(buf[16:20]))

		// Quick sanity guard: offset must fit in the hour.
		if msOffset >= 3600*1000 {
			return fmt.Errorf("bad msOffset=%d in %s (decoder misaligned?)", msOffset, path)
		}

		t := Tick{
			TsUnixMS: baseUnixMS + int64(msOffset),
			Ask:      int32(askU),
			Bid:      int32(bidU),
			AskVol:   askVol,
			BidVol:   bidVol,
		}

		// Optional sanity guard for EURUSD-ish scaled 1e5:
		// (disable if you want multi-symbol generic)
		// if t.Ask < 50000 || t.Ask > 250000 { ... }

		if err := fn(t); err != nil {
			return err
		}
	}
}
