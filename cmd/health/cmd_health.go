// Package health provides CLI commands that query the trader serve REST API
// for health and version information.
package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	traderpkg "github.com/rustyeddy/trader"
)

var serverURL string

// New returns the top-level "health" cobra command.
func New(rc *traderpkg.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check the health and version of a running trader serve",
	}
	cmd.PersistentFlags().StringVar(&serverURL, "server", defaultServer(), "trader serve base URL")
	cmd.AddCommand(healthCheckCmd(), versionCmd())
	return cmd
}

func defaultServer() string {
	if u := os.Getenv("TRADER_SERVER"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

func healthCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Ping the health endpoint and print status",
		RunE: func(cmd *cobra.Command, args []string) error {
			var result map[string]string
			if err := apiGet(serverURL+"/health", &result); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "status: %s\n", result["status"])
			return nil
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version reported by a running trader serve",
		RunE: func(cmd *cobra.Command, args []string) error {
			var result map[string]string
			if err := apiGet(serverURL+"/api/v1/version", &result); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "server version: %s\n", result["version"])
			return nil
		},
	}
}

func apiGet(url string, out any) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
