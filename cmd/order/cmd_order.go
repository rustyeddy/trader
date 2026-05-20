package order

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/spf13/cobra"
)

var (
	instrument string
	side       string
	riskPct    float64
	stopPips   float64
	accountID  string
	token      string
	env        string
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "order",
		Short: "Live order management (OANDA demo)",
	}
	cmd.AddCommand(newOrderCmd())
	cmd.AddCommand(listOrdersCmd())
	return cmd
}

func newOrderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Size and submit a market order to OANDA demo with confirmation",
		RunE:  runNewOrder,
	}

	cmd.Flags().StringVar(&instrument, "instrument", "", "Instrument in OANDA format, e.g. USD_JPY (required)")
	cmd.Flags().StringVar(&side, "side", "", "Trade direction: long or short (required)")
	cmd.Flags().Float64Var(&riskPct, "risk-pct", 1.0, "Percent of account equity to risk per trade")
	cmd.Flags().Float64Var(&stopPips, "stop-pips", 0, "Stop distance in pips (optional; overrides ATR-based stop)")
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID (auto-discovered if omitted)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (falls back to ~/.config/oanda/pat.txt)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")

	_ = cmd.MarkFlagRequired("instrument")
	_ = cmd.MarkFlagRequired("side")

	return cmd
}

// readTokenFile reads the OANDA token from ~/.config/oanda/pat.txt if present.
func readTokenFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "oanda", "pat.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func runNewOrder(cmd *cobra.Command, args []string) error {
	// Resolve token: flag/env → ~/.config/oanda/pat.txt
	if token == "" {
		token = readTokenFile()
	}
	if token == "" {
		return fmt.Errorf("no OANDA token found — set OANDA_TOKEN, use --token, or save to ~/.config/oanda/pat.txt")
	}

	side = strings.ToLower(strings.TrimSpace(side))
	if side != "long" && side != "short" {
		return fmt.Errorf("--side must be 'long' or 'short', got %q", side)
	}

	baseURL, err := oanda.BaseURL(env)
	if err != nil {
		return err
	}

	client := &oanda.Client{
		BaseURL: baseURL,
		Token:   token,
	}

	ctx := context.Background()

	// ── 0. Auto-discover account ID if not provided ───────────────────────────
	if accountID == "" {
		accounts, err := client.GetAccounts(ctx)
		if err != nil {
			return fmt.Errorf("discover accounts: %w", err)
		}
		if len(accounts) == 0 {
			return fmt.Errorf("no accounts found for this token")
		}
		if len(accounts) > 1 {
			fmt.Println("Multiple accounts found — specify one with --account-id:")
			for _, a := range accounts {
				fmt.Printf("  %s\n", a.ID)
			}
			return fmt.Errorf("ambiguous account")
		}
		accountID = accounts[0].ID
		fmt.Printf("Using account: %s\n", accountID)
	}

	// ── 1. Fetch live price ───────────────────────────────────────────────────
	fmt.Printf("Fetching live price for %s...\n", instrument)
	prices, err := client.GetPricing(ctx, accountID, instrument)
	if err != nil {
		return fmt.Errorf("get pricing: %w", err)
	}
	if len(prices) == 0 {
		return fmt.Errorf("no price returned for %s", instrument)
	}
	px := prices[0]

	entryPrice := px.Ask // long enters at ask
	if side == "short" {
		entryPrice = px.Bid
	}

	// ── 2. Fetch account state ────────────────────────────────────────────────
	fmt.Println("Fetching account summary...")
	acct, err := client.GetAccountSummary(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get account: %w", err)
	}

	equity := acct.NAV
	if equity <= 0 {
		return fmt.Errorf("account equity is zero or unavailable")
	}

	// ── 3. Calculate stop price ───────────────────────────────────────────────
	var stopPrice float64
	var stopDesc string

	if stopPips > 0 {
		// Convert pips to price distance. For JPY pairs 1 pip = 0.01, others = 0.0001.
		pipSize := 0.0001
		if strings.Contains(instrument, "JPY") {
			pipSize = 0.01
		}
		dist := stopPips * pipSize
		if side == "long" {
			stopPrice = entryPrice - dist
		} else {
			stopPrice = entryPrice + dist
		}
		stopDesc = fmt.Sprintf("%.1f pips (%.5f)", stopPips, stopPrice)
	} else {
		return fmt.Errorf("--stop-pips is required (ATR-based stop coming in a future version)")
	}

	// ── 4. Size the position ──────────────────────────────────────────────────
	riskAmount := equity * (riskPct / 100.0)
	stopDist := math.Abs(entryPrice - stopPrice)
	if stopDist == 0 {
		return fmt.Errorf("stop distance is zero — check stop price")
	}

	// Units = risk amount / loss per unit
	// For non-JPY: 1 unit ≈ 1 USD per pip (roughly); actual P/L = units × price_change
	// Simple approximation: units = riskAmount / stopDist
	// For USD_JPY: P/L is in JPY, convert at current rate
	units := riskAmount / stopDist
	// OANDA uses micro-lots internally; round to whole units
	unitsInt := int64(math.Round(units))
	if unitsInt < 1 {
		unitsInt = 1
	}
	if side == "short" {
		unitsInt = -unitsInt
	}

	// ── 5. Display order for review ───────────────────────────────────────────
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────┐")
	fmt.Printf("│  PROPOSED ORDER — %s %-28s│\n", strings.ToUpper(env), "")
	fmt.Println("├─────────────────────────────────────────────────┤")
	fmt.Printf("│  Instrument   : %-32s│\n", instrument)
	fmt.Printf("│  Side         : %-32s│\n", strings.ToUpper(side))
	fmt.Printf("│  Entry price  : %-32.5f│\n", entryPrice)
	fmt.Printf("│  Stop price   : %-32s│\n", stopDesc)
	fmt.Printf("│  Units        : %-32d│\n", unitsInt)
	fmt.Println("├─────────────────────────────────────────────────┤")
	fmt.Printf("│  Account NAV  : $%-31.2f│\n", equity)
	fmt.Printf("│  Risk %%       : %-31.2f%%│\n", riskPct)
	fmt.Printf("│  Risk amount  : $%-31.2f│\n", riskAmount)
	fmt.Println("└─────────────────────────────────────────────────┘")
	fmt.Println()

	// ── 6. Confirm ───────────────────────────────────────────────────────────
	fmt.Print("Submit order? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("Order cancelled.")
		return nil
	}

	// ── 7. Submit ─────────────────────────────────────────────────────────────
	fmt.Println("Submitting order...")
	result, err := client.SubmitMarketOrder(ctx, accountID, instrument, unitsInt, stopPrice)
	if err != nil {
		return fmt.Errorf("submit order: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Order filled")
	fmt.Printf("  Order ID : %s\n", result.OrderID)
	fmt.Printf("  Trade ID : %s\n", result.TradeID)
	fmt.Printf("  Units    : %d\n", result.Units)
	fmt.Printf("  Price    : %.5f\n", result.Price)
	return nil
}

func listOrdersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List open trades from OANDA",
		RunE:  runListOrders,
	}
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID (auto-discovered if omitted)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (falls back to ~/.config/oanda/pat.txt)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	return cmd
}

func runListOrders(cmd *cobra.Command, args []string) error {
	if token == "" {
		token = readTokenFile()
	}
	if token == "" {
		return fmt.Errorf("no OANDA token found — set OANDA_TOKEN, use --token, or save to ~/.config/oanda/pat.txt")
	}

	baseURL, err := oanda.BaseURL(env)
	if err != nil {
		return err
	}

	client := &oanda.Client{BaseURL: baseURL, Token: token}
	ctx := context.Background()

	if accountID == "" {
		accounts, err := client.GetAccounts(ctx)
		if err != nil {
			return fmt.Errorf("discover accounts: %w", err)
		}
		if len(accounts) == 0 {
			return fmt.Errorf("no accounts found for this token")
		}
		if len(accounts) > 1 {
			fmt.Println("Multiple accounts found — specify one with --account-id:")
			for _, a := range accounts {
				fmt.Printf("  %s\n", a.ID)
			}
			return fmt.Errorf("ambiguous account")
		}
		accountID = accounts[0].ID
	}

	trades, err := client.GetOpenTrades(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get open trades: %w", err)
	}

	if len(trades) == 0 {
		fmt.Println("No open trades.")
		return nil
	}

	const width = 52
	bar := strings.Repeat("─", width)
	fmt.Println(bar)
	fmt.Printf("  %-10s %-10s %8s %12s %10s %10s\n", "Trade ID", "Instrument", "Units", "Entry", "Stop", "Unreal P/L")
	fmt.Println(bar)
	for _, t := range trades {
		stopStr := "—"
		if t.StopLoss > 0 {
			stopStr = fmt.Sprintf("%.5f", t.StopLoss)
		}
		fmt.Printf("  %-10s %-10s %8d %12.5f %10s %+10.2f\n",
			t.ID, t.Instrument, t.Units, t.EntryPrice, stopStr, t.UnrealizedPL)
	}
	fmt.Println(bar)
	return nil
}
