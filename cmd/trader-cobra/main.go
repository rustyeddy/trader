package main

import (
	"os"

	"github.com/rustyeddy/trader/cmd/trader-cobra/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
