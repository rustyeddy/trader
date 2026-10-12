package backtestcfg

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rustyeddy/trader/internal/config"
)

// This file is the part of the run-config grammar (ADR-077) that needs to
// know which keys were written rather than what they resolved to: the legacy
// grammar, the effective kind, and wrong-kind keys.

// readDocument returns the config file as a generic map, or nil when there is
// none or it cannot be read or parsed (config.Load reports those itself).
func readDocument(opts config.Options) map[string]any {
	data := opts.FileContent
	if data == nil && opts.FilePath != "" {
		b, err := os.ReadFile(opts.FilePath)
		if err != nil {
			return nil
		}
		data = b
	}
	var doc map[string]any
	if len(data) == 0 || yaml.Unmarshal(data, &doc) != nil {
		return nil
	}
	return doc
}

// legacyMarketKeys are the pre-#474 backtest.* keys that moved to market.*.
var legacyMarketKeys = []string{
	"symbol", "symbols", "interval", "from", "to", "provider", "data_raw_root", "data_store_root",
}

// rejectLegacyKeys reports a pre-#474 document (a top-level model: section,
// or market keys under backtest:) with the key's new home, instead of
// letting the loader ignore the unknown keys and fail with a confusing
// "market.from is required". The old grammar is not accepted (ADR-077).
func rejectLegacyKeys(doc map[string]any) error {
	if _, ok := doc["model"]; ok {
		return fmt.Errorf("the model: section was replaced by kind: study with a market: section and a study: section (ADR-077)")
	}
	bt, _ := doc["backtest"].(map[string]any)
	for _, k := range legacyMarketKeys {
		if _, ok := bt[k]; ok {
			to := k
			if k == "symbol" {
				to = "symbols"
			}
			return fmt.Errorf("backtest.%s moved to market.%s; set kind: and use a market: section (ADR-077)", k, to)
		}
	}
	return nil
}

// effectiveKind is the kind the loader will resolve, read from the sources
// in precedence order (overrides, environment, file) with backtest as the
// default; config.Load validates the value itself.
func effectiveKind(opts config.Options, doc map[string]any) string {
	if k, ok := opts.Overrides["kind"]; ok {
		return k
	}
	if k, ok := environ(opts)[envName(opts.EnvPrefix, "kind")]; ok {
		return k
	}
	if k, ok := doc["kind"].(string); ok && k != "" {
		return k
	}
	return KindBacktest
}

// routeStudyOverrides moves the existing --strategy-name/-exec/-config
// overrides to the study section, so a study's launch identity is
// overridable by the same flags a backtest's external strategy uses.
func routeStudyOverrides(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		switch k {
		case "strategy-name":
			k = "study-name"
		case "strategy-exec":
			k = "study-exec"
		case "strategy-config":
			k = "study-config"
		}
		out[k] = v
	}
	return out
}

// keyDef names a key that belongs to one kind: its dotted path and the
// override (flag) name that sets it.
type keyDef struct{ path, flag string }

// studyOnlyKeys must not be written in a backtest document.
var studyOnlyKeys = []keyDef{
	{"study.name", "study-name"}, {"study.exec", "study-exec"}, {"study.config", "study-config"},
}

// backtestOnlyKeys must not be written in a study document. The strategy
// keys that pick an in-process strategy or a buy-and-hold mode are here;
// exec and config are covered with a pointer to their study.* replacement.
var backtestOnlyKeys = []keyDef{
	{"backtest.currency", "currency"},
	{"backtest.starting_capital", "starting-cash"},
	{"backtest.risk_fraction", "risk-fraction"},
	{"backtest.adverse_distance", "adverse-distance"},
	{"backtest.initial_margin_ratio", "initial-margin-ratio"},
	{"strategy.name", "strategy-name"},
	{"strategy.fast_period", "fast-period"},
	{"strategy.slow_period", "slow-period"},
	{"strategy.allowed_side", "allowed-side"},
	{"strategy.quantity", "quantity"},
	{"strategy.buy_date", "buy-date"},
	{"strategy.sell_date", "sell-date"},
	{"strategy.exec", "strategy-exec"},
	{"strategy.config", "strategy-config"},
}

// studyReplacement names the study.* key to use instead of a strategy.* one.
var studyReplacement = map[string]string{
	"strategy.exec": "study.exec", "strategy.config": "study.config", "strategy.name": "study.name",
}

// checkSections rejects every key of the other kind that any source wrote, by
// presence: a value equal to the loader default (currency: USD) is still a
// mixed document. Defaults the host fills in afterwards are not "written".
func checkSections(kind string, present map[string]bool) error {
	switch kind {
	case KindStudy:
		for _, k := range backtestOnlyKeys {
			if !present[k.path] {
				continue
			}
			if use := studyReplacement[k.path]; use != "" {
				return fmt.Errorf("%s does not apply to kind: study; use %s", k.path, use)
			}
			return fmt.Errorf("%s does not apply to kind: study; remove it or use kind: backtest", k.path)
		}
	case KindBacktest:
		for _, k := range studyOnlyKeys {
			if present[k.path] {
				return fmt.Errorf("%s requires kind: study (this document is kind: backtest)", k.path)
			}
		}
	}
	return nil
}

// presentKeys returns the dotted paths of the checked keys that any source
// wrote: the file, the environment, or the overrides.
func presentKeys(opts config.Options, doc map[string]any) map[string]bool {
	out := map[string]bool{}
	env := environ(opts)
	for _, k := range append(append([]keyDef{}, studyOnlyKeys...), backtestOnlyKeys...) {
		section, key, _ := strings.Cut(k.path, ".")
		if sec, ok := doc[section].(map[string]any); ok {
			if _, ok := sec[key]; ok {
				out[k.path] = true
			}
		}
		if _, ok := env[envName(opts.EnvPrefix, k.path)]; ok {
			out[k.path] = true
		}
		if _, ok := opts.Overrides[k.flag]; ok {
			out[k.path] = true
		}
	}
	return out
}

func environ(opts config.Options) map[string]string {
	out := make(map[string]string, len(opts.Environ))
	for _, kv := range opts.Environ {
		if name, value, ok := strings.Cut(kv, "="); ok {
			out[name] = value
		}
	}
	return out
}

// envName mirrors internal/config's environment variable derivation:
// PREFIX_SECTION_KEY, or just SECTION_KEY with no prefix.
func envName(prefix, path string) string {
	name := strings.ToUpper(strings.ReplaceAll(path, ".", "_"))
	if prefix == "" {
		return name
	}
	return strings.ToUpper(prefix) + "_" + name
}
