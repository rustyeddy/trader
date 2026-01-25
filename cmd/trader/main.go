package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rustyeddy/trader/journal"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	sub := os.Args[1]
	switch sub {
	case "journal":
		os.Exit(journalCmd(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", sub)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `trader - small CLI utilities for the trader project

Usage:
  trader journal -db PATH trade <trade_id>
  trader journal -db PATH today
  trader journal -db PATH day YYYY-MM-DD

Examples:
  trader journal -db ./trader.sqlite trade 3f2b5c12-....
  trader journal -db ./trader.sqlite today
  trader journal -db ./trader.sqlite day 2026-01-24`)
}

func journalCmd(args []string) int {
	fs := flag.NewFlagSet("journal", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var dbPath string
	fs.StringVar(&dbPath, "db", "./trader.sqlite", "path to SQLite journal DB")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	rest := fs.Args()
	if len(rest) < 1 {
		usage()
		return 2
	}

	j, err := journal.NewSQLite(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		return 1
	}
	defer j.Close()

	loc := time.Local

	switch rest[0] {
	case "trade":
		if len(rest) != 2 {
			fmt.Fprintln(os.Stderr, "journal trade requires a trade_id")
			return 2
		}
		rec, err := j.GetTrade(rest[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		fmt.Println(journal.FormatTradeOrg(rec))
		return 0

	case "today":
		start, end, err := dayBounds(loc, time.Now().In(loc).Format("2006-01-02"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "date: %v\n", err)
			return 1
		}
		recs, err := j.ListTradesClosedBetween(start, end)
		if err != nil {
			fmt.Fprintf(os.Stderr, "query trades: %v\n", err)
			return 1
		}
		fmt.Println(journal.FormatTradesOrg(recs))
		return 0

	case "day":
		if len(rest) != 2 {
			fmt.Fprintln(os.Stderr, "journal day requires YYYY-MM-DD")
			return 2
		}
		start, end, err := dayBounds(loc, rest[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "date: %v\n", err)
			return 1
		}
		recs, err := j.ListTradesClosedBetween(start, end)
		if err != nil {
			fmt.Fprintf(os.Stderr, "query trades: %v\n", err)
			return 1
		}
		fmt.Println(journal.FormatTradesOrg(recs))
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown journal command: %s\n\n", rest[0])
		usage()
		return 2
	}
}

func dayBounds(loc *time.Location, day string) (time.Time, time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	end := start.Add(24 * time.Hour)
	return start, end, nil
}
