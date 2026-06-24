package docs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/rustyeddy/trader/config"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func New(_ *config.RootConfig) *cobra.Command {
	var (
		outFile string
		outDir  string
		format  string
		single  bool
	)

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate CLI reference documentation",
		Long: `Generate reference documentation for all trader commands.

By default all commands are written to a single combined file (docs/trader-cli.md).
Use --single=false to write one file per command into --out instead.

Supported formats:
  markdown  Combined file (default) or one .md file per command
  man       One man page per command (troff format; --single not supported)
  rst       One .rst file per command (reStructuredText; --single not supported)`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()

			if single && format != "markdown" && format != "md" {
				return fmt.Errorf("--single is only supported with --format markdown")
			}

			if single {
				// Write all commands into one combined file by walking the command
				// tree and calling doc.GenMarkdown for each node into the same writer.
				if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
					return fmt.Errorf("create output dir: %w", err)
				}
				f, err := os.Create(outFile)
				if err != nil {
					return fmt.Errorf("create %s: %w", outFile, err)
				}
				defer f.Close()

				if err := genMarkdownAll(root, f); err != nil {
					return fmt.Errorf("generate single-file markdown: %w", err)
				}

				fmt.Fprintf(cmd.OutOrStdout(), "docs written to %s\n", outFile)
				return nil
			}

			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("create output dir %s: %w", outDir, err)
			}

			switch format {
			case "markdown", "md":
				if err := doc.GenMarkdownTree(root, outDir); err != nil {
					return fmt.Errorf("generate markdown: %w", err)
				}
			case "man":
				header := &doc.GenManHeader{
					Title:   "TRADER",
					Section: "1",
					Source:  "trader",
				}
				if err := doc.GenManTree(root, header, outDir); err != nil {
					return fmt.Errorf("generate man pages: %w", err)
				}
			case "rst":
				if err := doc.GenReSTTree(root, outDir); err != nil {
					return fmt.Errorf("generate rst: %w", err)
				}
			default:
				return fmt.Errorf("unknown format %q: must be markdown, man, or rst", format)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "docs written to %s (format: %s)\n", outDir, format)
			return nil
		},
	}

	cmd.Flags().BoolVar(&single, "single", true, "Write all commands to a single combined file (see --file)")
	cmd.Flags().StringVar(&outFile, "file", "docs/trader-cli.md", "Output file path when --single is set")
	cmd.Flags().StringVar(&outDir, "out", "./docs", "Output directory when --single=false")
	cmd.Flags().StringVar(&format, "format", "markdown", "Output format: markdown, man, rst")

	return cmd
}

// genMarkdownAll recursively walks the command tree depth-first and writes
// each command's markdown section to w. Subcommands are sorted alphabetically
// so the output is stable across runs. The root command is written first,
// then each subtree in order.
func genMarkdownAll(cmd *cobra.Command, w io.Writer) error {
	// cobra marks the docs/help commands as hidden; skip them so they don't
	// pollute the reference output.
	if cmd.Hidden {
		return nil
	}

	if err := doc.GenMarkdown(cmd, w); err != nil {
		return err
	}

	// Sort subcommands so output order is deterministic.
	subs := cmd.Commands()
	sort.Slice(subs, func(i, j int) bool {
		return subs[i].Name() < subs[j].Name()
	})

	for _, sub := range subs {
		if err := genMarkdownAll(sub, w); err != nil {
			return err
		}
	}
	return nil
}
