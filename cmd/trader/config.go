package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
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
		return fileConfig{}, fmt.Errorf("config %s: %w", path, valueFree(err))
	}
	return cfg, nil
}

// yamlLine matches the "line N: " prefix the YAML library puts on a problem.
var yamlLine = regexp.MustCompile(`line (\d+)`)

// valueFree turns a YAML library error into one that cannot quote the file's
// values: the library's type errors repeat the offending value ("cannot
// unmarshal !!str `...` into int"), and a config file near a secret must never
// have a value reach a log. An unknown field keeps its name (a key, never a
// value); anything else keeps only its line number.
func valueFree(err error) error {
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return withLine(err.Error(), "invalid YAML")
	}
	var parts []string
	for _, e := range te.Errors {
		if i := strings.Index(e, "field "); i >= 0 && strings.Contains(e, "not found in type") {
			name, _, _ := strings.Cut(e[i+len("field "):], " ")
			parts = append(parts, withLine(e, "unknown key "+fmt.Sprintf("%q", name)).Error())
			continue
		}
		parts = append(parts, withLine(e, "a value has the wrong type").Error())
	}
	return errors.New(strings.Join(parts, "; "))
}

func withLine(raw, what string) error {
	if m := yamlLine.FindStringSubmatch(raw); m != nil {
		return fmt.Errorf("%s at line %s (details withheld: they could quote a value)", what, m[1])
	}
	return fmt.Errorf("%s (details withheld: they could quote a value)", what)
}

// secretWords are fragments of a key name that mark it as a secret.
var secretWords = []string{"token", "secret", "password", "passwd", "apikey", "api_key", "credential", "bearer", "authorization"}

// refuseSecretKeys reports the name (never the value) of any mapping key that
// looks like a secret, and rejects a file with more than one YAML document, so
// nothing after a "---" escapes the check. It reads the syntax tree only, which
// resolves no value, so a tagged or mistyped secret cannot surface in an error.
func refuseSecretKeys(data []byte) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil // an empty file
		}
		return valueFree(err)
	}
	var next yaml.Node
	if err := dec.Decode(&next); !errors.Is(err, io.EOF) {
		if err != nil {
			return valueFree(err)
		}
		return errors.New("more than one YAML document: only the first would be read, so the file is refused")
	}
	return walkKeys(&doc)
}

func walkKeys(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			lower := strings.ToLower(k.Value)
			for _, w := range secretWords {
				if strings.Contains(lower, w) {
					return fmt.Errorf("key %q looks like a secret: secrets are read from the environment only (TRADER_OANDA_TOKEN), never from this file", k.Value)
				}
			}
			if err := walkKeys(n.Content[i+1]); err != nil {
				return err
			}
		}
		return nil
	}
	for _, c := range n.Content {
		if err := walkKeys(c); err != nil {
			return err
		}
	}
	return nil
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
