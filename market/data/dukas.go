package data

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ulikunitz/xz/lzma"
)

const defaultBase = "https://datafeed.dukascopy.com/datafeed"

type job struct {
	url  string
	dst  string // .bi5 path
	flat string // .bin output path
}

func main() {
	var (
		base    = flag.String("base", defaultBase, "Dukascopy base URL")
		symbol  = flag.String("symbol", "EURUSD", "Symbol like EURUSD, USDJPY")
		start   = flag.String("start", "", "Start (UTC) like 2026-01-01T00")
		end     = flag.String("end", "", "End (UTC, exclusive) like 2026-01-02T00")
		one     = flag.String("one", "", "Single hour (UTC) like 2026-01-01T13 (overrides start/end)")
		outDir  = flag.String("out", "./dukas", "Output directory")
		workers = flag.Int("workers", max(4, runtime.NumCPU()), "Parallel workers")
		timeout = flag.Duration("timeout", 45*time.Second, "HTTP timeout")
		sleep   = flag.Duration("sleep", 50*time.Millisecond, "Polite delay per request")
	)
	flag.Parse()

	sym := strings.ToUpper(strings.TrimSpace(*symbol))
	if sym == "" {
		fatalf("symbol required")
	}

	var t0, t1 time.Time
	var err error

	if *one != "" {
		t0, err = time.ParseInLocation("2006-01-02T15", *one, time.UTC)
		if err != nil {
			fatalf("bad --one: %v", err)
		}
		t1 = t0.Add(time.Hour)
	} else {
		if *start == "" || *end == "" {
			fatalf("either --one or both --start and --end required")
		}
		t0, err = time.ParseInLocation("2006-01-02T15", *start, time.UTC)
		if err != nil {
			fatalf("bad --start: %v", err)
		}
		t1, err = time.ParseInLocation("2006-01-02T15", *end, time.UTC)
		if err != nil {
			fatalf("bad --end: %v", err)
		}
		if !t1.After(t0) {
			fatalf("--end must be after --start")
		}
	}

	// Build jobs
	var jobs []job
	for t := t0; t.Before(t1); t = t.Add(time.Hour) {
		url := dukasTickURL(*base, sym, t)
		bi5Path := filepath.Join(*outDir, sym, fmt.Sprintf("%04d", t.Year()), fmt.Sprintf("%02d", t.Month()), fmt.Sprintf("%02d", t.Day()), fmt.Sprintf("%02d", t.Hour())+"h_ticks.bi5")
		binPath := strings.TrimSuffix(bi5Path, ".bi5") + ".bin"
		jobs = append(jobs, job{url: url, dst: bi5Path, flat: binPath})
	}

	fmt.Printf("Symbol: %s\nRange:  %s -> %s (hours=%d)\nOut:    %s\n\n",
		sym, t0.Format(time.RFC3339), t1.Format(time.RFC3339), len(jobs), *outDir)

	client := &http.Client{Timeout: *timeout}
	ctx := context.Background()

	// Worker pool
	jobCh := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ok, miss, fail int

	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				time.Sleep(*sleep)

				// Download (skip if already present)
				downloaded, status, err := downloadIfMissing(ctx, client, j.url, j.dst)
				if err != nil {
					mu.Lock()
					fail++
					mu.Unlock()
					fmt.Printf("FAIL  %s  (%v)\n", j.url, err)
					continue
				}
				if status == 404 {
					mu.Lock()
					miss++
					mu.Unlock()
					fmt.Printf("404   %s\n", j.url)
					continue
				}

				// Decompress (skip if already present and bi5 wasn't freshly downloaded)
				if _, err := os.Stat(j.flat); err == nil && !downloaded {
					mu.Lock()
					ok++
					mu.Unlock()
					fmt.Printf("SKIP  %s (have .bin)\n", j.flat)
					continue
				}

				if err := decompressBI5(j.dst, j.flat); err != nil {
					mu.Lock()
					fail++
					mu.Unlock()
					fmt.Printf("FAIL  decompress %s (%v)\n", j.dst, err)
					continue
				}

				// Optional: print checksum of decompressed output (handy for cache validation)
				sum, _ := sha256File(j.flat)
				mu.Lock()
				ok++
				mu.Unlock()
				fmt.Printf("OK    %s  sha256=%s\n", j.flat, sum)
			}
		}()
	}

	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)
	wg.Wait()

	fmt.Printf("\nDone. ok=%d miss(404)=%d fail=%d\n", ok, miss, fail)
}

func dukasTickURL(base, symbol string, t time.Time) string {
	// Dukascopy uses zero-based month in URL path: Jan=00 ... Dec=11
	month0 := int(t.Month()) - 1
	return fmt.Sprintf("%s/%s/%04d/%02d/%02d/%02dh_ticks.bi5",
		strings.TrimRight(base, "/"),
		symbol,
		t.Year(), month0, t.Day(), t.Hour())
}

func downloadIfMissing(ctx context.Context, client *http.Client, url, dst string) (downloaded bool, status int, err error) {
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		return false, 200, nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, 0, err
	}

	tmp := dst + ".part"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("User-Agent", "go-duka-downloader/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, 404, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, resp.StatusCode, fmt.Errorf("http status %d", resp.StatusCode)
	}

	f, err := os.Create(tmp)
	if err != nil {
		return false, resp.StatusCode, err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return false, resp.StatusCode, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return false, resp.StatusCode, closeErr
	}

	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return false, resp.StatusCode, err
	}
	return true, resp.StatusCode, nil
}

func decompressBI5(srcBI5, dstBIN string) error {
	in, err := os.Open(srcBI5)
	if err != nil {
		return err
	}
	defer in.Close()

	r, err := lzma.NewReader(in)
	if err != nil {
		return err
	}

	tmp := dstBIN + ".part"
	if err := os.MkdirAll(filepath.Dir(dstBIN), 0o755); err != nil {
		return err
	}
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(out, r)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dstBIN)
}

// optional helper: sha256 of a file
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// (Unused) example if you later want gzip output rather than raw .bin
func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	gw := gzip.NewWriter(out)

	if _, err := io.Copy(gw, in); err != nil {
		_ = gw.Close()
		_ = out.Close()
		return err
	}
	if err := gw.Close(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
