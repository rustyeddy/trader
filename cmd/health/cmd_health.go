// Package health provides CLI commands that query the trader serve REST API
// for health and version information.
package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/rustyeddy/trader/config"
	"github.com/spf13/cobra"
)

var serverURL string

// New returns the top-level "health" cobra command.
func New(rc *config.RootConfig) *cobra.Command {
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

type healthResponse struct {
	Status string `json:"status"`
}

type versionResponse struct {
	Version string `json:"version"`
}

func healthCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Ping the health endpoint and print status",
		RunE: func(cmd *cobra.Command, args []string) error {
			var result healthResponse
			if err := apiGet(serverURL+"/health", &result); err != nil {
				return err
			}
			if result.Status == "" {
				return fmt.Errorf("server response missing status field")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "status: %s\n", result.Status)
			return nil
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version reported by a running trader serve",
		RunE: func(cmd *cobra.Command, args []string) error {
			var result versionResponse
			if err := apiGet(serverURL+"/api/v1/version", &result); err != nil {
				return err
			}
			if result.Version == "" {
				return fmt.Errorf("server response missing version field")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "server version: %s\n", result.Version)
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
		var errBody struct {
			Error string `json:"error"`
		}
		if jsonErr := json.NewDecoder(resp.Body).Decode(&errBody); jsonErr == nil && errBody.Error != "" {
			return fmt.Errorf("server returned %s: %s", resp.Status, errBody.Error)
		}
		return fmt.Errorf("server returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
