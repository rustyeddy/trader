package main

import (
	"context"
	"strings"
	"time"

	"github.com/rustyeddy/trader/data"
)

type Config struct {
	Start    time.Time
	End      time.Time
	Symbols  string
	Basedir  string
	Dukasdir string
}

var (
	config = &Config{
		Start:    time.Date(2003, 01, 01, 0, 0, 0, 0, time.UTC),
		End:      time.Now().AddDate(0, 0, -1), // start from yesterday (the last fullday)
		Symbols:  "EURUSD,USDJPY,GBPUSD",
		Basedir:  "../../tmp/",
		Dukasdir: "dukas/",
	}
)

func main() {
	dm := &data.DataManager{
		Start:       config.Start,
		End:         config.End,
		Basedir:     config.Basedir + config.Dukasdir,
		Instruments: strings.Split(config.Symbols, ","),
	}

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	dm.BuildDatasets(ctx)
}

// func createCandles(basedir string) {
// 	symbols := make(map[string]*data.Dataset)
// 	for _, inst := range market.InstrumentList {
// 		symbols[inst] = nil
// 	}

// 	var wg sync.WaitGroup
// 	err := filepath.WalkDir(basedir, func(path string, d fs.DirEntry, err error) error {
// 		if err != nil {
// 			fmt.Printf("preventing walk into a directory: %v\n", err)
// 			return err
// 		}

// 		// 2. Process the file or directory
// 		if d.IsDir() {
// 			return nil
// 		}
// 		df := &data.datafile{}
// 		err = df.parsePath(path)
// 		if err != nil {
// 			return err
// 		}

// 		m1Q := make(chan Tick)
// 		go func() {
// 			wg.Add(1)
// 			m1maker(m1Q)
// 			defer wg.Done()
// 		}()

// 		// fmt.Println(df.Path())
// 		ctx, _ := context.WithCancel(context.TODO())
// 		err = df.ForEachTick(ctx, func(t data.Tick) error {
// 			m1Q <- t

// 			// fmt.Printf("ITs a tick %+v\n", t)
// 			return nil
// 		})
// 		// fmt.Printf("ticks: %d\n", ticks)

// 		// 3. Return nil to continue the traversal
// 		return nil
// 	})

// 	wg.Wait()
// 	if err != nil {
// 		log.Fatalf("error during directory traversal: %s", err)
// 	}
// }

// func m1maker(m1Q <-chan data.Tick) {

// 	for t := range m1Q {
// 		fmt.Printf("TICK: %+v\n", t)
// 	}

// }

// func panicErr(err error) {
// 	if err != nil {
// 		panic(err)
// 	}
// }
