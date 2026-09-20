package config

import "strings"

// envValues turns process-environment-style "NAME=VALUE" strings (the
// format os.Environ returns, and the format Options.Environ expects) into a
// lookup map. An entry with no "=" is skipped rather than treated as a
// variable with an empty value, since it cannot occur in a real os.Environ
// result and most likely indicates a malformed test fixture.
func envValues(environ []string) map[string]string {
	out := make(map[string]string, len(environ))
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		out[name] = value
	}
	return out
}
