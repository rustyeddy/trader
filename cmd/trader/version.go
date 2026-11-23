package main

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// gitDescribe is the release this binary was built from, set with
// -ldflags "-X main.gitDescribe=..." by `make build` from
// `git describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*' --always --dirty`: "v0.5.0"
// at the tag, "v0.5.0-12-gabc1234" past it, "-dirty" with uncommitted changes.
var gitDescribe string

// version is what this binary reports: the tag it was built from, or when it was
// not built through make, the module version Go recorded (a `go install
// github.com/rustyeddy/trader/cmd/trader@v0.5.0` is exactly that tag).
func version() string {
	module := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		module = info.Main.Version
	}
	return resolveVersion(gitDescribe, module)
}

// resolveVersion prefers the git-describe string, then the module version, and
// otherwise says it is a development build: "(devel)" is Go's own placeholder for
// a binary with no version.
func resolveVersion(describe, module string) string {
	switch {
	case describe != "":
		return describe
	case module != "" && module != "(devel)":
		return module
	}
	return "devel"
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of trader",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "trader", version())
			return err
		},
	}
}
