package dukascopy

import "github.com/rustyeddy/trader/marketdata"

func init() {
	marketdata.Register(&Provider{})
}

// Provider implements marketdata.Provider for the Dukascopy datafeed.
type Provider struct{}

func (p *Provider) Name() string { return SourceName }

// SourceURL returns the Dukascopy datafeed URL for the given slice.
func (p *Provider) SourceURL(sp marketdata.SourceParams) string {
	return NewFile(sp.Instrument, sp.Time).URL()
}
