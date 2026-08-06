// Package config assembles typed application configuration for Trader's
// executable composition roots, as decided by issue #20 (M1-02).
//
// # Ownership
//
// config is a composition-root support package: it belongs to the binaries
// under cmd/ and to test/example programs that construct Trader
// applications, not to domain packages. Domain packages (num, and everything
// that follows it: instrument, marketdata, strategy, risk, broker, ...) must
// never read files, environment variables, or command-line flags themselves.
// They accept typed configuration values through their constructors instead.
// A composition root loads configuration once, at startup, with this
// package, and passes the resolved values down.
//
// Each component owns its own configuration struct. There is no single giant
// untyped settings tree: a strategy's config, a broker adapter's config, and
// a data source's config are separate types loaded (possibly independently)
// by whatever composition root needs them. config.Load is generic precisely
// so each caller supplies its own destination type.
//
// # Sources and precedence
//
// Load resolves each field from up to four sources, in this order, lowest
// precedence first:
//
//	defaults < file < environment < overrides
//
// A source that does not mention a field leaves the previous source's value
// in place. A field mentioned by no source at all keeps its Go zero value,
// unless a default tag was given (making "no source" equivalent to
// "default"), or the field is marked required, which is checked after every
// source has been applied.
//
// # Struct tags
//
// Fields are addressed by a dotted path derived from their position in the
// (possibly nested) destination struct, lowercased: a Port field inside a
// Server field has path "server.port". Tags override pieces of that
// derivation or add behavior:
//
//	config:"name"    overrides this field's path segment (default: lowercased Go field name)
//	env:"NAME"       overrides the environment variable name outright, no prefix applied
//	flag:"name"      overrides the Overrides map key (default: the dotted path with "." replaced by "-")
//	default:"value"  the value used when no source sets this field
//	enum:"a,b,c"     restricts an already-decoded string field to this list
//	required:"true"  Load fails if no source (including default) supplied this field a value
//	secret:"true"    this field's value is never written to error messages or Render output
//
// required is a presence check, not a zero-value check: an explicitly
// supplied false, 0, "", or an exact numeric zero such as num.Rate("0")
// satisfies it. A field with both required and default is not a
// contradiction — default just means the field is always satisfied — but
// combining them is usually redundant.
//
// # Environment variable naming convention
//
// Unless a field carries an explicit env tag, its environment variable name
// is EnvPrefix + "_" + the dotted path, uppercased, with "." replaced by "_":
// with EnvPrefix "TRADER", the field path "server.port" resolves to
// TRADER_SERVER_PORT. Trader's own binaries use the prefix "TRADER"; a
// reusable component embedded in someone else's composition root can choose
// its own prefix or none.
//
// # Supported field types
//
// string, bool, every sized int/uint, float32/float64, time.Duration,
// *url.URL, a string field restricted to a fixed set of values (via the
// enum tag; enum only constrains string-kind fields, and is silently
// ignored on any other kind), pointer fields for optional values (nil means
// "absent," decoded otherwise), and any type implementing
// encoding.TextUnmarshaler. There is no float64
// prohibition here as there is in num: config values are not authoritative
// trading data, and num.Price/num.Quantity/num.Rate/num.Money/num.Currency
// can be used directly as config field types since they already implement
// encoding.TextUnmarshaler.
//
// # Validation
//
// Load aggregates every field-level problem — parse failures, unsupported
// types, and missing required fields — into one returned error instead of
// stopping at the first one, so a misconfigured startup reports everything
// wrong in one pass. Use errors.Is against the sentinels in errors.go, or
// errors.As against *FieldError, to inspect individual failures.
//
// After every source is applied and every required field is checked, Load
// calls Validate() error on the destination if it implements that method,
// for validation that spans multiple fields (e.g. "either A or B, not
// both"). Invalid configuration prevents startup: Load returns before the
// caller ever sees a partially-valid value.
//
// # Determinism
//
// Load never reads the real process environment or an on-disk file unless
// the caller explicitly asks it to (Options.Environ nil, or a non-empty
// Options.FilePath). Tests should always set Options.Environ (even to an
// empty slice) and use Options.FileContent instead of Options.FilePath, so
// results do not depend on the developer's environment or working
// directory.
package config
