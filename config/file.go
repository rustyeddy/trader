package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// fileValues returns the flattened dotted-path -> raw-string values found in
// the YAML file or content named by opts, or nil if opts names neither.
// Paths are lowercased to match the lowercased default path derivation in
// fields.go, so config files are conventionally written in lowercase.
//
// A key whose YAML value is explicitly null is treated the same as a key
// that is absent from the file: it does not participate in this source, and
// resolution falls through to the environment, default, or the field's Go
// zero value.
func fileValues(opts Options) (map[string]string, error) {
	var data []byte
	switch {
	case opts.FileContent != nil:
		data = opts.FileContent
	case opts.FilePath != "":
		b, err := os.ReadFile(opts.FilePath)
		if err != nil {
			return nil, fmt.Errorf("config: reading %s: %w", opts.FilePath, err)
		}
		data = b
	default:
		return nil, nil
	}

	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("config: parsing YAML: %w", err)
	}

	out := map[string]string{}
	flattenYAML("", root, out)
	return out, nil
}

func flattenYAML(prefix string, node any, out map[string]string) {
	m, ok := node.(map[string]any)
	if !ok {
		if node == nil {
			return
		}
		out[prefix] = fmt.Sprint(node)
		return
	}

	for k, v := range m {
		key := strings.ToLower(k)
		if prefix != "" {
			key = prefix + "." + key
		}
		flattenYAML(key, v, out)
	}
}
