package datamanager

import (
	"github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/market"
)

// Plan describes the data-preparation work that must be completed before a
// backtest can run: files to download and candle aggregations to build at
// each timeframe.
type Plan struct {
	Download []Key
	BuildM1  []BuildTask
	BuildH1  []BuildTask
	BuildD1  []BuildTask
}

func (p *Plan) downloadCount() int {
	if p == nil {
		return 0
	}
	return len(p.Download)
}

func (p *Plan) buildCount(tf market.Timeframe) int {
	return len(p.BuildTasks(tf))
}

func (p *Plan) TotalBuilds() int {
	return p.buildCount(market.M1) + p.buildCount(market.H1) + p.buildCount(market.D1)
}

func (p *Plan) Empty() bool {
	return p.downloadCount() == 0 && p.TotalBuilds() == 0
}

func (p *Plan) BuildTasks(tf market.Timeframe) []BuildTask {
	if p == nil {
		return nil
	}
	switch tf {
	case market.M1:
		return p.BuildM1
	case market.H1:
		return p.BuildH1
	case market.D1:
		return p.BuildD1
	default:
		return nil
	}
}

// Log emits a structured summary of the plan (download and build counts) at
// info level.
func (p *Plan) Log() {
	log.Info("plan summary",
		"downloads", p.downloadCount(),
		"build_m1", p.buildCount(market.M1),
		"build_h1", p.buildCount(market.H1),
		"build_d1", p.buildCount(market.D1),
		"build_total", p.TotalBuilds(),
	)
}

// BuildTask represents a single candle-aggregation job: build the candles
// identified by Key from the listed input Keys.
type BuildTask struct {
	Key
	Inputs []Key
}
