# AGENTS.md Instructions

Trader is a framework to test and execute algorithmic trading.

# Trader Agent Instructions

Read before making changes:

- `docs/arch/trader-framework-architecture.org`
- `docs/arch/trader-framework-requirements.org`
- `docs/arch/adr-decisions.org`
- `docs/arch/package-boundaries.org`
- `CONTRIBUTING.org`

Detailed workflows:

- `docs/workflows/workflows.org`
- `docs/workflows/testing.org` (not yet populated)

## Development

- We will stick to idiomatic go as much as possible
- Features and bug fixes will be done on short lived branches
- The branch will be pushed to origin and a PR created
- Code reviews on all new code will be reviewed
- Code and PR's must be associated with a corresponding github issue 

### Logging

- We will use structured logging with the go log/slog package
- Typical log levels will be used fatal, error, warning, info and debug. 
- Logger by default will log to /var/log/trader.log 
- Logger flags will be available for 
  - log output file, default: /var/log/trader.log
  - Output flag for writing to stderr or stdout
  - Flag to change the log level: From Fatal to Debug
  - Flag to change the output format includes: text (human readable) or JSON

## Testing 

### Unit Tests

- For all new or changed code must be accompanied by testing.
- We will use the Go testify package for testing
- We will shoot for test coverage of 85% or greater
- Tests need to target corner, edge and failure mode completely

### System Tests

TBD
