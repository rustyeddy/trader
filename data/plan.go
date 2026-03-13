package data

type Plan struct {
	Download       []AssetKey
	BuildM1        []AssetKey
	BuildH1        []AssetKey
	BuildD1        []AssetKey
	TickHoursReady []AssetKey
}
