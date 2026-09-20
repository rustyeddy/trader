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
// Values are taken from each scalar node's literal text, not from an
// intermediate Go value: unmarshaling into map[string]any would first
// convert an unquoted numeric scalar to a float64, and a float64 cannot
// exactly represent every decimal text an exact type like num.Price or
// num.Rate needs — 92233720368.54775807 loses precision at 15-17
// significant digits, and some values switch to exponent notation ("1e-08")
// that num's exact parser rejects outright. Config must not silently route
// authoritative decimal text through binary floating point merely because
// it happened to pass through this package on the way from YAML text to a
// TextUnmarshaler.
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

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("config: parsing YAML: %w", err)
	}

	out := map[string]string{}
	if len(doc.Content) == 0 {
		return out, nil // empty document
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config: parsing YAML: top-level document must be a mapping")
	}

	flattenYAMLNode("", root, out)
	return out, nil
}

// flattenYAMLNode walks a YAML mapping node, recording each scalar leaf's
// literal text under its dotted path. node.Content on a mapping node
// alternates key node, value node, key node, value node, ...
func flattenYAMLNode(prefix string, node *yaml.Node, out map[string]string) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]

		key := strings.ToLower(keyNode.Value)
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		switch valNode.Kind {
		case yaml.MappingNode:
			flattenYAMLNode(path, valNode, out)
		case yaml.ScalarNode:
			if valNode.Tag == "!!null" {
				continue // explicit null: absent from this source, not a value
			}
			out[path] = valNode.Value
		default:
			// Sequences and other node kinds have no matching leaf type
			// (see fields.go); leaving them out of the map is equivalent to
			// "no leaf will ever look this path up."
		}
	}
}
