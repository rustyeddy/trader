package backtest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/broker"
)

func TestParseTickRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		row       []string
		wantOk    bool
		wantErr   bool
		checkFunc func(t *testing.T, p broker.Price)
	}{
		{
			name:    "valid row",
			row:     []string{"2026-01-24T09:30:00Z", "EUR_USD", "1.1000", "1.1002"},
			wantOk:  true,
			wantErr: false,
			checkFunc: func(t *testing.T, p broker.Price) {
				if p.Instrument != "EUR_USD" {
					t.Errorf("instrument = %v, want EUR_USD", p.Instrument)
				}
				if p.Bid != 1.1000 {
					t.Errorf("bid = %v, want 1.1000", p.Bid)
				}
				if p.Ask != 1.1002 {
					t.Errorf("ask = %v, want 1.1002", p.Ask)
				}
			},
		},
		{
			name:    "valid row with nano timestamp",
			row:     []string{"2026-01-24T09:30:00.123456789Z", "GBP_USD", "1.2500", "1.2502"},
			wantOk:  true,
			wantErr: false,
			checkFunc: func(t *testing.T, p broker.Price) {
				if p.Instrument != "GBP_USD" {
					t.Errorf("instrument = %v, want GBP_USD", p.Instrument)
				}
			},
		},
		{
			name:    "row with whitespace",
			row:     []string{" 2026-01-24T09:30:00Z ", " EUR_USD ", " 1.1000 ", " 1.1002 "},
			wantOk:  true,
			wantErr: false,
			checkFunc: func(t *testing.T, p broker.Price) {
				if p.Instrument != "EUR_USD" {
					t.Errorf("instrument = %v, want EUR_USD", p.Instrument)
				}
			},
		},
		{
			name:    "too few columns",
			row:     []string{"2026-01-24T09:30:00Z", "EUR_USD", "1.1000"},
			wantOk:  false,
			wantErr: false,
		},
		{
			name:    "empty row",
			row:     []string{},
			wantOk:  false,
			wantErr: false,
		},
		{
			name:    "empty timestamp",
			row:     []string{"", "EUR_USD", "1.1000", "1.1002"},
			wantOk:  false,
			wantErr: false,
		},
		{
			name:    "empty instrument",
			row:     []string{"2026-01-24T09:30:00Z", "", "1.1000", "1.1002"},
			wantOk:  false,
			wantErr: false,
		},
		{
			name:    "invalid timestamp",
			row:     []string{"not-a-time", "EUR_USD", "1.1000", "1.1002"},
			wantOk:  false,
			wantErr: true,
		},
		{
			name:    "invalid bid",
			row:     []string{"2026-01-24T09:30:00Z", "EUR_USD", "not-a-number", "1.1002"},
			wantOk:  false,
			wantErr: true,
		},
		{
			name:    "invalid ask",
			row:     []string{"2026-01-24T09:30:00Z", "EUR_USD", "1.1000", "not-a-number"},
			wantOk:  false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p, ok, err := parseTickRow(tt.row)

			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}

			if ok && tt.checkFunc != nil {
				tt.checkFunc(t, p)
			}
		})
	}
}

func TestInRange(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 24, 12, 0, 0, 0, time.UTC)
	before := base.Add(-1 * time.Hour)
	after := base.Add(1 * time.Hour)

	tests := []struct {
		name string
		t    time.Time
		from time.Time
		to   time.Time
		want bool
	}{
		{
			name: "no range",
			t:    base,
			from: time.Time{},
			to:   time.Time{},
			want: true,
		},
		{
			name: "within range",
			t:    base,
			from: before,
			to:   after,
			want: true,
		},
		{
			name: "before range",
			t:    before,
			from: base,
			to:   after,
			want: false,
		},
		{
			name: "after range",
			t:    after,
			from: before,
			to:   base,
			want: false,
		},
		{
			name: "at from boundary",
			t:    base,
			from: base,
			to:   after,
			want: true,
		},
		{
			name: "at to boundary",
			t:    base,
			from: before,
			to:   base,
			want: false,
		},
		{
			name: "only from constraint",
			t:    after,
			from: base,
			to:   time.Time{},
			want: true,
		},
		{
			name: "only to constraint",
			t:    before,
			from: time.Time{},
			to:   base,
			want: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := inRange(tt.t, tt.from, tt.to)
			if got != tt.want {
				t.Errorf("inRange() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCSVTicksFeed_NewAndClose(t *testing.T) {
	t.Parallel()

	t.Run("successful creation", func(t *testing.T) {
		t.Parallel()

		tmp := t.TempDir()
		csvPath := filepath.Join(tmp, "test.csv")

		csv := `time,instrument,bid,ask
2026-01-24T09:30:00Z,EUR_USD,1.1000,1.1002
`
		if err := os.WriteFile(csvPath, []byte(csv), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		feed, err := NewCSVTicksFeed(csvPath, time.Time{}, time.Time{})
		if err != nil {
			t.Fatalf("NewCSVTicksFeed: %v", err)
		}
		defer feed.Close()

		if feed.f == nil {
			t.Error("expected f to be set")
		}
		if feed.r == nil {
			t.Error("expected r to be set")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		t.Parallel()

		_, err := NewCSVTicksFeed("/nonexistent/path.csv", time.Time{}, time.Time{})
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})

	t.Run("close without file", func(t *testing.T) {
		t.Parallel()

		feed := &CSVTicksFeed{}
		err := feed.Close()
		if err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	})
}

func TestCSVTicksFeed_Next(t *testing.T) {
	t.Parallel()

	t.Run("basic iteration", func(t *testing.T) {
		t.Parallel()

		tmp := t.TempDir()
		csvPath := filepath.Join(tmp, "test.csv")

		csv := `time,instrument,bid,ask
2026-01-24T09:30:00Z,EUR_USD,1.1000,1.1002
2026-01-24T09:30:05Z,EUR_USD,1.1010,1.1012
2026-01-24T09:30:10Z,EUR_USD,1.1020,1.1022
`
		if err := os.WriteFile(csvPath, []byte(csv), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		feed, err := NewCSVTicksFeed(csvPath, time.Time{}, time.Time{})
		if err != nil {
			t.Fatalf("NewCSVTicksFeed: %v", err)
		}
		defer feed.Close()

		// Read all ticks
		var ticks []broker.Price
		for {
			p, ok, err := feed.Next()
			if err != nil {
				t.Fatalf("Next() error: %v", err)
			}
			if !ok {
				break
			}
			ticks = append(ticks, p)
		}

		if len(ticks) != 3 {
			t.Errorf("got %d ticks, want 3", len(ticks))
		}

		if len(ticks) >= 1 && ticks[0].Bid != 1.1000 {
			t.Errorf("first tick bid = %v, want 1.1000", ticks[0].Bid)
		}
	})

	t.Run("skip header", func(t *testing.T) {
		t.Parallel()

		tmp := t.TempDir()
		csvPath := filepath.Join(tmp, "test.csv")

		csv := `time,instrument,bid,ask
2026-01-24T09:30:00Z,EUR_USD,1.1000,1.1002
`
		if err := os.WriteFile(csvPath, []byte(csv), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		feed, err := NewCSVTicksFeed(csvPath, time.Time{}, time.Time{})
		if err != nil {
			t.Fatalf("NewCSVTicksFeed: %v", err)
		}
		defer feed.Close()

		p, ok, err := feed.Next()
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		if !ok {
			t.Fatal("expected to get a tick")
		}

		// Header should be skipped, so we get the data row
		if p.Instrument != "EUR_USD" {
			t.Errorf("instrument = %v, want EUR_USD", p.Instrument)
		}
	})

	t.Run("filter by time range", func(t *testing.T) {
		t.Parallel()

		tmp := t.TempDir()
		csvPath := filepath.Join(tmp, "test.csv")

		csv := `2026-01-24T09:30:00Z,EUR_USD,1.1000,1.1002
2026-01-24T09:30:05Z,EUR_USD,1.1010,1.1012
2026-01-24T09:30:10Z,EUR_USD,1.1020,1.1022
2026-01-24T09:30:15Z,EUR_USD,1.1030,1.1032
`
		if err := os.WriteFile(csvPath, []byte(csv), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		from := time.Date(2026, 1, 24, 9, 30, 5, 0, time.UTC)
		to := time.Date(2026, 1, 24, 9, 30, 15, 0, time.UTC)

		feed, err := NewCSVTicksFeed(csvPath, from, to)
		if err != nil {
			t.Fatalf("NewCSVTicksFeed: %v", err)
		}
		defer feed.Close()

		var ticks []broker.Price
		for {
			p, ok, err := feed.Next()
			if err != nil {
				t.Fatalf("Next() error: %v", err)
			}
			if !ok {
				break
			}
			ticks = append(ticks, p)
		}

		// Should get 2 ticks: 09:30:05 and 09:30:10 (from is inclusive, to is exclusive)
		if len(ticks) != 2 {
			t.Errorf("got %d ticks, want 2", len(ticks))
		}
	})

	t.Run("skip empty rows", func(t *testing.T) {
		t.Parallel()

		tmp := t.TempDir()
		csvPath := filepath.Join(tmp, "test.csv")

		csv := `2026-01-24T09:30:00Z,EUR_USD,1.1000,1.1002

2026-01-24T09:30:05Z,EUR_USD,1.1010,1.1012
`
		if err := os.WriteFile(csvPath, []byte(csv), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		feed, err := NewCSVTicksFeed(csvPath, time.Time{}, time.Time{})
		if err != nil {
			t.Fatalf("NewCSVTicksFeed: %v", err)
		}
		defer feed.Close()

		var ticks []broker.Price
		for {
			p, ok, err := feed.Next()
			if err != nil {
				t.Fatalf("Next() error: %v", err)
			}
			if !ok {
				break
			}
			ticks = append(ticks, p)
		}

		if len(ticks) != 2 {
			t.Errorf("got %d ticks, want 2", len(ticks))
		}
	})

	t.Run("skip short rows", func(t *testing.T) {
		t.Parallel()

		tmp := t.TempDir()
		csvPath := filepath.Join(tmp, "test.csv")

		csv := `2026-01-24T09:30:00Z,EUR_USD,1.1000,1.1002
2026-01-24T09:30:05Z,EUR_USD
2026-01-24T09:30:10Z,EUR_USD,1.1020,1.1022
`
		if err := os.WriteFile(csvPath, []byte(csv), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		feed, err := NewCSVTicksFeed(csvPath, time.Time{}, time.Time{})
		if err != nil {
			t.Fatalf("NewCSVTicksFeed: %v", err)
		}
		defer feed.Close()

		var ticks []broker.Price
		for {
			p, ok, err := feed.Next()
			if err != nil {
				t.Fatalf("Next() error: %v", err)
			}
			if !ok {
				break
			}
			ticks = append(ticks, p)
		}

		if len(ticks) != 2 {
			t.Errorf("got %d ticks, want 2", len(ticks))
		}
	})

	t.Run("empty file", func(t *testing.T) {
		t.Parallel()

		tmp := t.TempDir()
		csvPath := filepath.Join(tmp, "test.csv")

		if err := os.WriteFile(csvPath, []byte(""), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		feed, err := NewCSVTicksFeed(csvPath, time.Time{}, time.Time{})
		if err != nil {
			t.Fatalf("NewCSVTicksFeed: %v", err)
		}
		defer feed.Close()

		p, ok, err := feed.Next()
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		if ok {
			t.Errorf("expected ok=false for empty file, got tick: %+v", p)
		}
	})
}
