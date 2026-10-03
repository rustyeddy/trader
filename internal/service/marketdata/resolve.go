package marketdata

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/num"
)

// KindFX is the InstrumentIdentity kind of an FX pair.
const KindFX = "fx"

var (
	// ErrResolverNotConfigured reports instrument resolution on a
	// Service constructed without WithResolver.
	ErrResolverNotConfigured = errors.New("service/marketdata: instrument resolver is not configured")
	// ErrListingConflict reports a registration whose provider, venue,
	// and symbol are already registered to a different instrument.
	ErrListingConflict = errors.New("service/marketdata: a different listing is already registered")
)

// IsFXProvider reports whether provider's instruments are FX pairs.
// OANDA is the one FX provider; every other provider (stooq, alpaca, or
// an unrecognized one) is treated as equity/ETF, since guessing FX for
// an unknown provider would be the more surprising default.
func IsFXProvider(provider string) bool { return provider == "oanda" }

// InstrumentRequest names an instrument by symbol. Exchange and Kind
// (KindEquity or KindETF) apply only to equity/ETF providers: given
// together they override the symbol's DefaultListing, and they are
// ignored for an FX provider.
type InstrumentRequest struct {
	Symbol   string
	Exchange string
	Kind     string
}

// InstrumentIdentity is what an InstrumentRequest identifies: a
// normalized symbol, its kind (KindFX, KindEquity, or KindETF), and,
// for equities and ETFs, its listing exchange.
type InstrumentIdentity struct {
	Symbol   string
	Kind     string
	Exchange string
}

// IdentifyInstrument decides what req names for a data provider: an FX
// pair for an FX provider, otherwise an equity or ETF from req's
// explicit exchange and kind or the symbol's DefaultListing. It fails
// with ErrInvalidRequest for an empty symbol, an incomplete identity
// (ErrIncompleteListingIdentity), or an unsupported kind, and with
// ErrNoListingDefault for an equity symbol with neither.
func IdentifyInstrument(provider string, req InstrumentRequest) (InstrumentIdentity, error) {
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if symbol == "" {
		return InstrumentIdentity{}, fmt.Errorf("%w: symbol is required", ErrInvalidRequest)
	}
	if IsFXProvider(provider) {
		return InstrumentIdentity{Symbol: symbol, Kind: KindFX}, nil
	}
	ld, err := ResolveListingIdentity(symbol, req.Exchange, req.Kind)
	if err != nil {
		return InstrumentIdentity{}, err
	}
	return InstrumentIdentity{Symbol: symbol, Kind: ld.Kind, Exchange: ld.Exchange}, nil
}

// RegisterIdentity registers id's Listing under provider in resolver and
// returns it. The provider need not be the one id was identified for: a
// backtest registers the same identity under its data provider and under
// its simulated broker. Registration is idempotent (ADR-069 Decision 2).
func RegisterIdentity(resolver *instrument.MemoryResolver, provider string, id InstrumentIdentity) (instrument.Listing, error) {
	if resolver == nil {
		return instrument.Listing{}, ErrResolverNotConfigured
	}
	var (
		listing instrument.Listing
		err     error
	)
	switch id.Kind {
	case KindFX:
		listing, err = fxListing(provider, id.Symbol)
	case KindEquity, KindETF:
		listing, err = equityIdentityListing(provider, id)
	default:
		return instrument.Listing{}, fmt.Errorf("%w: unsupported instrument kind %q", ErrInvalidRequest, id.Kind)
	}
	if err != nil {
		return instrument.Listing{}, err
	}
	registered, err := registerListing(resolver, listing)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("registering instrument %q: %w", id.Symbol, err)
	}
	return registered, nil
}

// RegisterInstrument identifies req for provider and registers it there:
// the one instrument-resolution path every transport shares (ADR-069
// Decision 1). It returns the registered Listing.
func RegisterInstrument(resolver *instrument.MemoryResolver, provider string, req InstrumentRequest) (instrument.Listing, error) {
	id, err := IdentifyInstrument(provider, req)
	if err != nil {
		return instrument.Listing{}, err
	}
	return RegisterIdentity(resolver, provider, id)
}

// equityIdentityListing builds the Listing for an equity or ETF
// identity, in USD (ADR-047's one Phase 1 settlement currency).
func equityIdentityListing(provider string, id InstrumentIdentity) (instrument.Listing, error) {
	reg := EquityRegistration{Provider: provider, Exchange: id.Exchange, Ticker: id.Symbol, Currency: num.MustParseCurrency("USD")}
	newInst := instrument.NewEquity
	if id.Kind == KindETF {
		newInst = instrument.NewETF
	}
	inst, err := newInst(id.Exchange, id.Symbol)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("invalid %s %s:%s: %w", id.Kind, id.Exchange, id.Symbol, err)
	}
	return equityLikeListing(reg, inst)
}

// registerListing registers listing in resolver idempotently through
// MemoryResolver.RegisterOrGet: if its exact provider, venue, and symbol
// are already registered to the same instrument, the existing Listing is
// returned. A different instrument under the same key fails with
// ErrListingConflict.
func registerListing(resolver *instrument.MemoryResolver, listing instrument.Listing) (instrument.Listing, error) {
	registered, err := resolver.RegisterOrGet(listing)
	if errors.Is(err, instrument.ErrDuplicateListing) {
		return instrument.Listing{}, fmt.Errorf("%w: %w", ErrListingConflict, err)
	}
	return registered, err
}

// InstrumentResponse is one resolved instrument.
type InstrumentResponse struct {
	Identity   InstrumentIdentity
	Instrument instrument.ID
	Listing    instrument.Listing
}

// ResolveInstrument resolves req under the Service's provider and
// registers it in the Service's resolver, so the Service's other
// operations can then act on Instrument. Calling it again for the same
// instrument succeeds. Once the symbol is non-empty, it logs exactly one
// outcome record.
func (s *Service) ResolveInstrument(ctx context.Context, req InstrumentRequest) (resp InstrumentResponse, err error) {
	if strings.TrimSpace(req.Symbol) == "" {
		return InstrumentResponse{}, fmt.Errorf("%w: symbol is required", ErrInvalidRequest)
	}
	defer func() {
		attrs := []any{"provider", s.provider, "symbol", req.Symbol}
		if err != nil {
			s.logger.ErrorContext(ctx, "instrument resolution failed", append(attrs, "error", err)...)
			return
		}
		s.logger.DebugContext(ctx, "instrument resolved", append(attrs, logging.InstrumentID, resp.Instrument.String())...)
	}()
	return s.resolveInstrument(ctx, req)
}

// resolveInstrument is ResolveInstrument without its outcome log, so
// ResolveInstruments can log once for the whole request.
func (s *Service) resolveInstrument(ctx context.Context, req InstrumentRequest) (InstrumentResponse, error) {
	if err := ctx.Err(); err != nil {
		return InstrumentResponse{}, err
	}
	if s.resolver == nil {
		return InstrumentResponse{}, ErrResolverNotConfigured
	}
	id, err := IdentifyInstrument(s.provider, req)
	if err != nil {
		return InstrumentResponse{}, err
	}
	listing, err := RegisterIdentity(s.resolver, s.provider, id)
	if err != nil {
		return InstrumentResponse{}, err
	}
	return InstrumentResponse{Identity: id, Instrument: listing.InstrumentID(), Listing: listing}, nil
}

// ResolveInstrumentsRequest resolves several instruments at once.
type ResolveInstrumentsRequest struct {
	Instruments []InstrumentRequest
}

// InstrumentResult is one request's outcome: a resolved instrument, or
// the error that request alone failed with.
type InstrumentResult struct {
	Request InstrumentRequest
	InstrumentResponse
	Err error
}

// ResolveInstrumentsResponse holds one InstrumentResult per request, in
// request order.
type ResolveInstrumentsResponse struct {
	Results []InstrumentResult
}

// ResolveInstruments resolves every request independently, so one bad
// symbol never hides the others: a failure is recorded on its own
// result. Repeated symbols resolve to the same instrument. The call
// itself fails only for an empty request, a missing resolver, or ctx.
// After the empty-request check it logs exactly one aggregate record,
// whichever way it exits.
func (s *Service) ResolveInstruments(ctx context.Context, req ResolveInstrumentsRequest) (resp ResolveInstrumentsResponse, err error) {
	if len(req.Instruments) == 0 {
		return ResolveInstrumentsResponse{}, fmt.Errorf("%w: at least one instrument is required", ErrInvalidRequest)
	}
	failed := 0
	defer func() {
		attrs := []any{"provider", s.provider, "requested", len(req.Instruments), "failed", failed}
		if err != nil {
			s.logger.ErrorContext(ctx, "instrument resolution failed", append(attrs, "error", err)...)
			return
		}
		s.logger.DebugContext(ctx, "instruments resolved", attrs...)
	}()
	if s.resolver == nil {
		return ResolveInstrumentsResponse{}, ErrResolverNotConfigured
	}
	resp = ResolveInstrumentsResponse{Results: make([]InstrumentResult, 0, len(req.Instruments))}
	for _, r := range req.Instruments {
		if err := ctx.Err(); err != nil {
			return ResolveInstrumentsResponse{}, err
		}
		out, err := s.resolveInstrument(ctx, r)
		if err != nil {
			failed++
		}
		resp.Results = append(resp.Results, InstrumentResult{Request: r, InstrumentResponse: out, Err: err})
	}
	return resp, nil
}
