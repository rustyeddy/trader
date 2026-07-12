package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/rustyeddy/trader/review"
)

// GlobalConfig holds settings that apply across all trader commands.
// It is populated by merging YAML files from the standard search path
// in order:
//
//  1. /etc/trader/*.yml        — system-wide defaults
//  2. ~/.config/trader/*.yml   — user overrides
//  3. explicit path            — passed via root --config flag
//
// Within each directory, files are merged alphabetically. Later files
// override earlier ones for any non-empty field.
type GlobalConfig struct {
	Log    GlobalLogConfig    `yaml:"log"`
	Data   GlobalDataConfig   `yaml:"data"`
	OANDA  GlobalOANDAConfig  `yaml:"oanda"`
	Review GlobalReviewConfig `yaml:"review"`
	DB     string             `yaml:"db"`
}

// GlobalLogConfig holds log-related global settings.
type GlobalLogConfig struct {
	Level  string `yaml:"level"`
	File   string `yaml:"file"`
	Format string `yaml:"format"`
}

// GlobalDataConfig holds data directory settings.
type GlobalDataConfig struct {
	Dir string `yaml:"dir"`
}

// GlobalOANDAConfig holds OANDA broker credentials.
type GlobalOANDAConfig struct {
	Token     string `yaml:"token"`
	AccountID string `yaml:"account_id"`
	Env       string `yaml:"env"`
}

// GlobalReviewConfig holds `trader review`'s triage thresholds (see
// review.Thresholds and GitHub issue #165). A zero-valued field means
// "not configured" and falls back to review.DefaultThresholds() via
// ToThresholds.
type GlobalReviewConfig struct {
	HotD1ADXFloor        float64 `yaml:"hot_d1_adx_floor"`
	HotD1CICeiling       float64 `yaml:"hot_d1_ci_ceiling"`
	TradeableH4CICeiling float64 `yaml:"tradeable_h4_ci_ceiling"`
	H4ADXFloor           float64 `yaml:"h4_adx_floor"`
	H4MinEMASep          float64 `yaml:"h4_min_ema_sep"`
	DemotionD1ADXFloor   float64 `yaml:"demotion_d1_adx_floor"`
	DemotionD1CICeiling  float64 `yaml:"demotion_d1_ci_ceiling"`
	WeekUsedCaution      float64 `yaml:"week_used_caution"`
	ValueZoneMin         float64 `yaml:"value_zone_min"`
	ValueZoneMax         float64 `yaml:"value_zone_max"`
	H4SqueezeWidthATR    float64 `yaml:"h4_squeeze_width_atr"`
}

// ToThresholds converts c into a review.Thresholds, using
// review.DefaultThresholds() for any field c leaves at zero.
func (c GlobalReviewConfig) ToThresholds() review.Thresholds {
	return review.MergeThresholds(review.DefaultThresholds(), review.Thresholds{
		HotD1ADXFloor:        c.HotD1ADXFloor,
		HotD1CICeiling:       c.HotD1CICeiling,
		TradeableH4CICeiling: c.TradeableH4CICeiling,
		H4ADXFloor:           c.H4ADXFloor,
		H4MinEMASep:          c.H4MinEMASep,
		DemotionD1ADXFloor:   c.DemotionD1ADXFloor,
		DemotionD1CICeiling:  c.DemotionD1CICeiling,
		WeekUsedCaution:      c.WeekUsedCaution,
		ValueZoneMin:         c.ValueZoneMin,
		ValueZoneMax:         c.ValueZoneMax,
		H4SqueezeWidthATR:    c.H4SqueezeWidthATR,
	})
}

// standardGlobalDirs returns the ordered list of directories to search
// for global config files.
func standardGlobalDirs() []string {
	dirs := []string{"/etc/trader"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", "trader"))
	}
	return dirs
}

// LoadGlobalConfig merges global config files from the standard search
// path plus an optional explicit file. Missing directories and files are
// silently skipped; a parse error in any file is returned immediately.
func LoadGlobalConfig(explicitPath string) (*GlobalConfig, error) {
	return loadGlobalConfig(standardGlobalDirs(), explicitPath)
}

// loadGlobalConfig is the testable core: accepts an explicit dir list
// instead of computing them from the filesystem.
func loadGlobalConfig(dirs []string, explicitPath string) (*GlobalConfig, error) {
	merged := &GlobalConfig{}

	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.yml"))
		if err != nil {
			return nil, err
		}
		sort.Strings(files)
		for _, f := range files {
			if err := mergeGlobalConfigFile(merged, f); err != nil {
				return nil, err
			}
		}
	}

	if explicitPath != "" {
		if err := mergeGlobalConfigFile(merged, explicitPath); err != nil {
			return nil, err
		}
	}

	return merged, nil
}

func mergeGlobalConfigFile(dst *GlobalConfig, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("global config: read %q: %w", path, err)
	}
	var src GlobalConfig
	if err := yaml.Unmarshal(data, &src); err != nil {
		return fmt.Errorf("global config: parse %q: %w", path, err)
	}
	mergeGlobalConfig(dst, &src)
	return nil
}

// mergeGlobalConfig copies non-empty fields from src into dst.
func mergeGlobalConfig(dst, src *GlobalConfig) {
	if src.Log.Level != "" {
		dst.Log.Level = src.Log.Level
	}
	if src.Log.File != "" {
		dst.Log.File = src.Log.File
	}
	if src.Log.Format != "" {
		dst.Log.Format = src.Log.Format
	}
	if src.Data.Dir != "" {
		dst.Data.Dir = src.Data.Dir
	}
	if src.OANDA.Token != "" {
		dst.OANDA.Token = src.OANDA.Token
	}
	if src.OANDA.AccountID != "" {
		dst.OANDA.AccountID = src.OANDA.AccountID
	}
	if src.OANDA.Env != "" {
		dst.OANDA.Env = src.OANDA.Env
	}
	if src.DB != "" {
		dst.DB = src.DB
	}
	mergeGlobalReviewConfig(&dst.Review, &src.Review)
}

// mergeGlobalReviewConfig copies non-zero threshold fields from src into
// dst, mirroring mergeGlobalConfig's "non-empty/non-zero wins" convention.
func mergeGlobalReviewConfig(dst, src *GlobalReviewConfig) {
	if src.HotD1ADXFloor != 0 {
		dst.HotD1ADXFloor = src.HotD1ADXFloor
	}
	if src.HotD1CICeiling != 0 {
		dst.HotD1CICeiling = src.HotD1CICeiling
	}
	if src.TradeableH4CICeiling != 0 {
		dst.TradeableH4CICeiling = src.TradeableH4CICeiling
	}
	if src.H4ADXFloor != 0 {
		dst.H4ADXFloor = src.H4ADXFloor
	}
	if src.H4MinEMASep != 0 {
		dst.H4MinEMASep = src.H4MinEMASep
	}
	if src.DemotionD1ADXFloor != 0 {
		dst.DemotionD1ADXFloor = src.DemotionD1ADXFloor
	}
	if src.DemotionD1CICeiling != 0 {
		dst.DemotionD1CICeiling = src.DemotionD1CICeiling
	}
	if src.WeekUsedCaution != 0 {
		dst.WeekUsedCaution = src.WeekUsedCaution
	}
	if src.ValueZoneMin != 0 {
		dst.ValueZoneMin = src.ValueZoneMin
	}
	if src.ValueZoneMax != 0 {
		dst.ValueZoneMax = src.ValueZoneMax
	}
	if src.H4SqueezeWidthATR != 0 {
		dst.H4SqueezeWidthATR = src.H4SqueezeWidthATR
	}
}
