package trader

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
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
	Log   GlobalLogConfig   `yaml:"log"`
	Data  GlobalDataConfig  `yaml:"data"`
	OANDA GlobalOANDAConfig `yaml:"oanda"`
	DB    string            `yaml:"db"`
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
}
