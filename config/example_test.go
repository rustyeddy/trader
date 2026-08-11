package config_test

import (
	"fmt"

	"github.com/rustyeddy/trader/config"
)

// Example loads a typed configuration struct from an explicit set of
// sources — never a real environment or file path in an example — and
// renders it. A secret-tagged field's real value is loaded correctly
// but always rendered as config.Redacted, regardless of what it is.
func Example() {
	type serverConfig struct {
		Host   string `default:"0.0.0.0"`
		Port   int    `default:"8080"`
		APIKey string `config:"api_key" secret:"true" required:"true"`
	}

	cfg, err := config.Load[serverConfig](config.Options{
		Environ:     []string{},
		FileContent: []byte("api_key: sk_live_deadbeef\n"),
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(config.Sprint(cfg))
	// Output:
	// api_key = REDACTED
	// host = 0.0.0.0
	// port = 8080
}
