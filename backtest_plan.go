package trader

// Plan describes the data-preparation work that must be completed before a
// backtest can run: files to download and candle aggregations to build at
// each timeframe. Blocked entries list tasks that could not be scheduled due
// to missing inputs.
type Plan struct {
	Download []Key
	BuildM1  []BuildTask
	BuildH1  []BuildTask
	BuildD1  []BuildTask

	BlockedM1 []BuildDecision
	BlockedH1 []BuildDecision
	BlockedD1 []BuildDecision
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
	// Range  TimeRange
	Inputs []Key
	Kind   BuildKind
}

// BuildKind identifies the aggregation step to perform.
type BuildKind string

const (
	BuildM1 BuildKind = "m1_from_ticks" // aggregate tick data into M1 candles
	BuildH1 BuildKind = "h1_from_m1"   // aggregate M1 candles into H1 candles
	BuildD1 BuildKind = "d1_from_h1"   // aggregate H1 candles into D1 candles
)

// WorkState tracks which downloads and build tasks are currently queued or
// running, preventing duplicate work from being scheduled.
type WorkState struct {
	activeDownloads map[Key]struct{}
	activeBuilds    map[Key]struct{}
}

// NewWorkState returns an empty WorkState with initialised internal maps.
func NewWorkState() *WorkState {
	return &WorkState{
		activeDownloads: make(map[Key]struct{}),
		activeBuilds:    make(map[Key]struct{}),
	}
}

// IsDownloadQueuedOrActive reports whether a download for k is already tracked.
func (ws *WorkState) IsDownloadQueuedOrActive(k Key) bool {
	_, ok := ws.activeDownloads[k]
	return ok
}

// IsBuildQueuedOrActive reports whether a build for k is already tracked.
func (ws *WorkState) IsBuildQueuedOrActive(k Key) bool {
	_, ok := ws.activeBuilds[k]
	return ok
}

// MarkDownload registers k as an active download.
func (ws *WorkState) MarkDownload(k Key) {
	ws.activeDownloads[k] = struct{}{}
}

// MarkBuild registers k as an active build.
func (ws *WorkState) MarkBuild(k Key) {
	ws.activeBuilds[k] = struct{}{}
}

// ClearDownload removes k from the active-downloads set (call on completion or error).
func (ws *WorkState) ClearDownload(k Key) {
	delete(ws.activeDownloads, k)
}

// ClearBuild removes k from the active-builds set (call on completion or error).
func (ws *WorkState) ClearBuild(k Key) {
	delete(ws.activeBuilds, k)
}
