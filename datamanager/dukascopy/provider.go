package dukascopy

import "github.com/rustyeddy/trader/datamanager"

func init() {
	datamanager.Register(&Provider{})
}

// Provider implements datamanager.Provider for the Dukascopy datafeed.
type Provider struct{}

func (p *Provider) Name() string { return SourceName }

// SourceURL returns the Dukascopy datafeed URL for the given slice.
func (p *Provider) SourceURL(sp datamanager.SourceParams) string {
	return NewFile(sp.Instrument, sp.Time).URL()
}
