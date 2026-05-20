package dukascopy

import (
	"github.com/rustyeddy/trader/data"
)

func init() {
	data.Register(&Provider{})
}

// Provider implements data.Provider for the Dukascopy datafeed.
type Provider struct{}

func (p *Provider) Name() string { return SourceName }

// SourceURL returns the Dukascopy datafeed URL for the given slice.
func (p *Provider) SourceURL(sp data.SourceParams) string {
	return NewFile(sp.Instrument, sp.Time).URL()
}
