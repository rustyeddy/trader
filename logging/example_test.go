package logging_test

import (
	"context"
	"fmt"
	"os"

	"github.com/rustyeddy/trader/logging"
)

// ExampleNew shows typical composition-root wiring: build a Config
// (normally via config.Load[logging.Config]), build a logger from it, and
// defer closing whatever it opened.
//
// This example is not output-verified: TextHandler's default output
// includes a wall-clock timestamp, which would make the expected output
// different on every run. See ExampleCapture for a fully verified example.
func ExampleNew() {
	cfg := logging.Config{
		Level:  0, // slog.LevelInfo
		Format: "text",
		Output: "stdout",
	}

	logger, closer, err := logging.New(cfg)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = closer.Close() }()

	logger.Info("server started", "port", 8080)
}

// ExampleCapture shows the test-kit pattern for asserting on structured log
// output without parsing formatted console text.
func ExampleCapture() {
	logger, rec := logging.Capture()

	logger.Info("order placed", logging.OrderID, "abc123")

	for _, r := range rec.Records() {
		fmt.Println(r.Message, r.Attrs[logging.OrderID])
	}
	// Output:
	// order placed abc123
}

// ExampleSecret shows redacting a sensitive value at the call site, the
// package's primary redaction mechanism.
func ExampleSecret() {
	logger, rec := logging.Capture()

	logger.Info("authenticated", "password", logging.Secret("hunter2"))

	fmt.Println(rec.Records()[0].Attrs["password"])
	// Output:
	// REDACTED
}

// ExampleWithCorrelationID shows propagating a correlation ID through
// context.Context so every record logged while handling one logical
// operation can be grouped together, without passing the attribute
// explicitly at every call site.
func ExampleWithCorrelationID() {
	logger, rec := logging.Capture()

	ctx := logging.WithCorrelationID(context.Background(), "corr-123")
	logger.InfoContext(ctx, "processing order")

	fmt.Println(rec.Records()[0].Attrs[logging.CorrelationID])
	// Output:
	// corr-123
}

// ExampleWithComponent shows scoping a logger to one subsystem, so its
// records stay distinguishable from other subsystems after aggregation.
func ExampleWithComponent() {
	logger, rec := logging.Capture()

	broker := logging.WithComponent(logger, "broker")
	broker.Info("connected")

	fmt.Println(rec.Records()[0].Attrs[logging.Component])
	// Output:
	// broker
}

// ExampleDiscard shows building a valid logger for a component test that
// doesn't care about log output.
func ExampleDiscard() {
	logger := logging.Discard()
	logger.Info("this goes nowhere")
	_, _ = fmt.Fprintln(os.Stdout, "component ran without a real logger")
	// Output:
	// component ran without a real logger
}
