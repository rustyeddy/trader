package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rustyeddy/trader/data"
	"github.com/rustyeddy/trader/types"
)

type Config struct {
	Symbols         string `"json":symbols`
	Start           string `"json":start`
	End             string `"json":end`
	types.Timeframe `"json":timeframe`
	Basedir         string `"json":basedir`
	Dukasdir        string `"json":dukasdir`
	Candledir       string `"json":candledir`
}

var (
	config = &Config{
		Symbols:   "EURUSD,USDJPY,GBPUSD",
		Start:     "",
		End:       "",
		Timeframe: types.D1,
		Basedir:   "../../tmp/",
		Dukasdir:  "dukas/",
	}
)

func init() {
	flag.StringVar(&config.Start, "start", "2025-01-01T00:00:00Z", "start of range")
	flag.StringVar(&config.End, "end", "2025-01-01T00:00:00Z", "end of range")
	flag.StringVar(&config.Symbols, "symbols", "USDJPY,GBPUSD", "Instruments to download")
	flag.StringVar(&config.Basedir, "basedir", "../../tmp/dukas", "Basedirectory")
}

func main() {
	start := time.Now()

	flag.Parse()
	// if len(os.Args) < 2 {
	// 	fmt.Println("Please give me a command (validate|build)")
	// 	os.Exit(1)
	// }

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	dm := &data.DataManager{
		Start:       time.Date(2004, 01, 01, 0, 0, 0, 0, time.UTC),
		End:         time.Now().AddDate(0, 0, -1), // start from yesterday (the last fullday)
		Basedir:     config.Basedir + config.Dukasdir,
		Instruments: strings.Split(config.Symbols, ","),
		Store: &data.CandleStore{
			Basedir: config.Candledir,
			Source:  "Dukascopy",
		},
		DukasRoot:  "../../tmp/dukas",
		Downloader: data.NewDownloader(),
	}
	dm.Init()
	if err := dm.Sync(ctx); err != nil {
		log.Fatal(err)
	}
	elapsed := time.Since(start)
	fmt.Printf("Program duration: %s\n", elapsed)
}
