package trader

// Plan describes the data-preparation work that must be completed before a
// backtest can run: files to download and candle aggregations to build at
// each timeframe.
type Plan struct {
	Download []Key
	BuildM1  []BuildTask
	BuildH1  []BuildTask
	BuildD1  []BuildTask
}

// Log emits a structured summary of the plan (download and build counts) at
// info level.
func (p Plan) Log() {
	Info("plan summary",
		"downloads", len(p.Download),
		"build_m1", len(p.BuildM1),
		"build_h1", len(p.BuildH1),
		"build_d1", len(p.BuildD1),
	)
}

// BuildTask represents a single candle-aggregation job: build the candles
// identified by Key from the listed input Keys using the specified Kind.
type BuildTask struct {
	Key
	Inputs []Key
	Kind   BuildKind
}

// BuildKind identifies the aggregation step to perform.
type BuildKind string

const (
	BuildM1 BuildKind = "m1_from_ticks" // aggregate tick data into M1 candles
	BuildH1 BuildKind = "h1_from_m1"    // aggregate M1 candles into H1 candles
	BuildD1 BuildKind = "d1_from_h1"    // aggregate H1 candles into D1 candles
)
