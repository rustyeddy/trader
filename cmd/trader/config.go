package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// defaultConfigPath is the repository's etc directory, kept simple for now
// (ADR-089).
const defaultConfigPath = "etc/trader.yml"

// fileConfig is etc/trader.yml. It never holds a secret: the OANDA token is
// read from the environment only (ADR-078 S-1), and a key that looks like a
// secret is an error rather than ignored.
type fileConfig struct {
	Marketdata marketdataConfig `yaml:"marketdata"`
}

type marketdataConfig struct {
	Root        string   `yaml:"root"`
	Provider    string   `yaml:"provider"`
	BaseURL     string   `yaml:"base_url"`
	Instruments []string `yaml:"instruments"`
	Interval    string   `yaml:"interval"`
}

// loadConfig reads path. A missing file is not an error unless required (the
// operator named it with --config): a flag or the environment can supply every
// value. An unknown key or a key that looks like a secret is an error; neither
// the error nor anything else repeats a value from the file.
func loadConfig(path string, required bool) (fileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !required {
			return fileConfig{}, nil
		}
		return fileConfig{}, fmt.Errorf("config %s: %w", path, err)
	}
	if err := refuseSecretKeys(data); err != nil {
		return fileConfig{}, fmt.Errorf("config %s: %w", path, err)
	}
	var cfg fileConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return fileConfig{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// secretWords are fragments of a key name that mark it as a secret.
var secretWords = []string{"token", "secret", "password", "passwd", "apikey", "api_key", "credential", "bearer", "authorization"}

// refuseSecretKeys reports the name (never the value) of any mapping key in the
// document that looks like a secret.
func refuseSecretKeys(data []byte) error {
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	var walk func(v any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				lower := strings.ToLower(k)
				for _, w := range secretWords {
					if strings.Contains(lower, w) {
						return fmt.Errorf("key %q looks like a secret: secrets are read from the environment only (TRADER_OANDA_TOKEN), never from this file", k)
					}
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range x {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(doc)
}

// resolveRoot picks the data root: the flag, then TRADER_DATA_ROOT, then the
// file. With none it fails and names all three, so the operator knows where to
// set it. Nothing hard-codes a default root.
func resolveRoot(flag, env, file string) (string, error) {
	for _, v := range []string{flag, env, file} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v), nil
		}
	}
	return "", errors.New("no data root: set --data-root, TRADER_DATA_ROOT, or marketdata.root in " + defaultConfigPath)
}
