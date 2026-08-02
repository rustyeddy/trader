# AGENTS.md Instructions

Trader is a framework to test and execute algorithmic trading.

# Trader Agent Instructions

Read before making changes:

- `docs/arch/trader-framework-architecture.org`
- `docs/arch/trader-framework-requirements.org`
- `docs/arch/adr-decisions.org`

Planned (not yet in repo):

- docs/arch/package-boundaries.org
- CONTRIBUTING.org

Detailed workflows:

- `docs/workflows/workflows.org`
- `docs/workflows/testing.org`

## Testing 

- For all new or changed code must be accompanied by testing.
- We will use the Go testify package for testing
- We will shoot for test coverage of 85% or greater
- Tests need to target corner, edge and failure mode completely
