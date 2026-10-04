package marketdata

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnknownProvider reports a provider name with no registered
// ProviderInfo. Trader never guesses an unknown provider's asset class,
// calendar, or capabilities.
var ErrUnknownProvider = errors.New("marketdata: unknown provider")

// AssetClass is the kind of instrument a provider's data covers.
type AssetClass uint8

const (
	// AssetClassFX is spot FX currency pairs.
	AssetClassFX AssetClass = iota + 1
	// AssetClassUSEquity is US-listed equities and ETFs.
	AssetClassUSEquity
)

func (a AssetClass) String() string {
	switch a {
	case AssetClassFX:
		return "fx"
	case AssetClassUSEquity:
		return "us-equity"
	default:
		return fmt.Sprintf("AssetClass(%d)", uint8(a))
	}
}

// CalendarKind names the trading calendar a provider's bars align to.
type CalendarKind uint8

const (
	// CalendarFX is FXCalendar's continuous weekly session (ADR-012).
	CalendarFX CalendarKind = iota + 1
	// CalendarUSEquity is USEquityCalendar's regular sessions (ADR-049).
	CalendarUSEquity
)

func (c CalendarKind) String() string {
	switch c {
	case CalendarFX:
		return "fx"
	case CalendarUSEquity:
		return "us-equity"
	default:
		return fmt.Sprintf("CalendarKind(%d)", uint8(c))
	}
}

// Credentials is what a provider's live acquisition authenticates with.
type Credentials uint8

const (
	// CredentialsNone: no live acquisition, so nothing to authenticate.
	CredentialsNone Credentials = iota
	// CredentialsToken is a single bearer token (OANDA).
	CredentialsToken
	// CredentialsKeyPair is a key ID and secret key (Alpaca).
	CredentialsKeyPair
)

// ProviderInfo is everything Trader knows about a market-data provider
// beyond its adapter implementation (issue #441): the one authoritative
// place composition and services read provider facts from, instead of
// inferring them from the provider's name.
//
// Adding a provider means one registration here plus its adapter (raw
// layout, normalization, and any acquisition client) dispatched by Name
// inside this package.
type ProviderInfo struct {
	// Name is the provider's identifier, as configured (TRADER_PROVIDER)
	// and as recorded in manifests and listings.
	Name string
	// AssetClass is the kind of instrument the provider's data covers;
	// it decides how a symbol is identified (an FX pair, or an equity or
	// ETF listing).
	AssetClass AssetClass
	// Calendar is the trading calendar its bars align to.
	Calendar CalendarKind
	// LiveAcquisition reports whether Sync can download raw data, so
	// Plan may schedule downloads and extends.
	LiveAcquisition bool
	// Credentials is what live acquisition authenticates with.
	Credentials Credentials
	// NativeArchive reports that raw data arrives by converting the
	// provider's downloaded native archive (ImportStooqArchive) rather
	// than by live acquisition.
	NativeArchive bool
}

// providers is the registry, sorted by Name.
var providers = []ProviderInfo{
	{Name: "alpaca", AssetClass: AssetClassUSEquity, Calendar: CalendarUSEquity, LiveAcquisition: true, Credentials: CredentialsKeyPair},
	{Name: "oanda", AssetClass: AssetClassFX, Calendar: CalendarFX, LiveAcquisition: true, Credentials: CredentialsToken},
	{Name: "stooq", AssetClass: AssetClassUSEquity, Calendar: CalendarUSEquity, Credentials: CredentialsNone, NativeArchive: true},
}

// LookupProvider returns name's ProviderInfo, or ErrUnknownProvider.
func LookupProvider(name string) (ProviderInfo, error) {
	for _, p := range providers {
		if p.Name == name {
			return p, nil
		}
	}
	return ProviderInfo{}, fmt.Errorf("%w: %q (supported: %s)", ErrUnknownProvider, name, strings.Join(ProviderNames(), ", "))
}

// Providers returns every registered provider, sorted by name.
func Providers() []ProviderInfo {
	return append([]ProviderInfo(nil), providers...)
}

// ProviderNames returns every registered provider's name, sorted.
func ProviderNames() []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name
	}
	return names
}
