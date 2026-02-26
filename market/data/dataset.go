package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rustyeddy/trader/types"
)

const defaultBase = "https://datafeed.dukascopy.com/datafeed"

type datasets map[string]*dataset

type dataset struct {
	instrument string // EURUSD, USDJPY, etc.
	start      string
	end        string
	basedir    string // root where data is to be stored
	baseurl    string // base url for the data
	one        string // one signle download

	datafiles []datafile

	workers int
	timeout time.Duration
	sleep   time.Duration
}

func (ds *dataset) gatherFiles(base string) {
	ds.basedir = base

	ds.datafiles = make([]datafile, 0, (2025-2016+1)*12*31*24)

	for year := 2025; year > 2015; year-- {
		for month := 0; month < 12; month++ {
			ndays := types.DaysInMonth(year, month)
			for day := 1; day <= ndays; day++ {
				for hour := 0; hour < 24; hour++ {

					ds.datafiles = append(ds.datafiles, datafile{
						instrument: ds.instrument,
						basedir:    base,
						year:       year,
						month:      month, // still 0-indexed for Dukascopy
						day:        day,
						hour:       hour,
					})

				}
			}
		}
	}
}

type datafile struct {
	instrument string
	year       int
	month      int
	day        int
	hour       int

	basedir string
	bytes   int64
	modtime time.Time
}

func (d *datafile) URL() string {
	return fmt.Sprintf(
		"https://datafeed.dukascopy.com/datafeed/%s/%04d/%02d/%02d/%02dh_ticks.bi5",
		d.instrument,
		d.year,
		d.month,
		d.day,
		d.hour)
}

func (d *datafile) Path() string {
	return filepath.Join(
		d.basedir,
		d.instrument,
		fmt.Sprintf("%04d", d.year),
		fmt.Sprintf("%02d", d.month),
		fmt.Sprintf("%02d", d.day),
		fmt.Sprintf("%02dh_ticks.bi5", d.hour),
	)
}

func (d *datafile) PathBin() string {
	return filepath.Join(d.basedir, fmt.Sprintf(
		"%s/%04d/%02d/%02d/%02dh_ticks.bin",
		d.instrument, d.year, d.month, d.day, d.hour,
	))
}

func (d *datafile) rawFileExists() bool {
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

func (d *datafile) download(ctx context.Context) error {
	// Skip if present
	if d.rawFileExists() {
		return nil
	}

	dst := d.Path()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}

	tmp := dst + ".part"

	// Correctness-first timeout
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	fmt.Printf("Downloading: %s...", d.URL())
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, d.URL(), nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", d.URL(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: http %d", d.URL(), resp.StatusCode)
	}

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

	fmt.Printf("%d bytes\n", n)

	// Optional: sanity-check bytes against what we copied
	if d.bytes != n || n == 0 {
		fmt.Printf("Failed to download: %s\n", d.URL())
	}

	return nil
}
