# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ChatGPT CLI is a Go-based multi-provider command-line interface for working with modern LLMs (OpenAI, Azure, Perplexity, LLaMA, etc.). It supports streaming, interactive chat, prompt files, image/audio I/O, MCP tool calls, and experimental agent mode for multi-step tasks with safety and budget controls.

**Core Technologies:**
- Go 1.24.1
- Cobra (CLI commands and flags)
- Viper (configuration management)
- sclevine/spec (test structure)
- gomega (test assertions)
- Zap (structured logging)

## Development Commands

### Build & Install
```bash
make install              # Build binaries for current OS
make install TARGET_OS=linux  # Build for specific OS
make reinstall            # Clean rebuild and install
make binaries             # Build for all platforms
```

On Windows, use:
```powershell
.\scripts\install.ps1
```

Alternatively, build directly with Go:
```bash
go build -mod=vendor -o build/chatgpt.exe ./cmd/chatgpt
```

### Using the `gpt` Shorthand (Windows)
A `gpt.bat` shim is available in `C:\Users\david\bin\gpt.bat` that allows you to use the shorter `gpt` command instead of the full `chatgpt` command:

```bash
gpt "what is 2+2?"
gpt --interactive
gpt --help
```

The shim simply forwards all arguments to the full chatgpt-cli executable.

### Testing (Critical)
```bash
make unit                 # Run unit tests
make integration          # Run integration tests
make contract             # Run contract tests
make all-tests            # Run all tests + lint/format/tidy
make coverage             # Generate combined coverage report
make smoke                # Run smoke tests
```

**Important:** Always run both `make unit` and `make integration` before committing changes. Integration tests are NOT optional.

To run tests for a specific package:
```bash
make unit ARGS=path/to/package
```

### Other Commands
```bash
make help                 # List all available targets
make updatedeps           # Update dependencies (runs go mod tidy)
make shipit version=X.Y.Z message="msg"  # Release process
```

### MCP Test Servers
```bash
make mcp-http             # Run local FastMCP HTTP server
make mcp-sse              # Run local FastMCP SSE server
```

## Architecture Overview

### Directory Structure

- **`cmd/chatgpt/`** - Main CLI entry point, Cobra command definitions, flag wiring
  - `main.go` - Application bootstrap, command setup
  - `utils/` - CLI-specific utilities

- **`internal/`** - Core non-exported application logic
  - `constants.go` - Application-wide constants
  - `logging.go` - Logging configuration
  - `utils.go` - General utilities
  - `fsio/` - File system I/O utilities

- **`api/`** - API clients and domain types for LLM providers
  - `client/` - HTTP client implementation with history management
  - `http/` - HTTP layer abstractions
  - `completions.go`, `responses.go` - API endpoint handlers
  - `mcp.go` - Model Context Protocol support

- **`config/`** - Configuration management (Viper integration)
  - `config.go` - Configuration data structures
  - `manager.go` - Configuration loading/merging logic
  - `store.go` - Configuration persistence
  - `completions.go` - Shell completion support

- **`history/`** - Conversation history tracking
  - `manager.go` - History CRUD operations
  - `store.go` - History file persistence

- **`cache/`** - LLM response caching
  - `cache.go` - Cache implementation
  - `store.go` - Cache persistence

- **`agent/`** - Autonomous agent system (experimental)
  - **`factory/`** - Agent factory (creates ReAct or Plan/Execute agents)
  - **`core/`** - Shared agent infrastructure
    - `base_agent.go` - Common agent functionality
    - `budget.go` - Budget tracking (steps, tokens, time, tool calls)
    - `policy.go` - Safety policy enforcement
    - `runner.go` - Step execution engine
    - `log.go` - Agent-specific logging
  - **`react/`** - ReAct agent (iterative think→act→observe loop)
  - **`planexec/`** - Plan/Execute agent (plan first, then execute steps)
  - **`tools/`** - Agent tools (shell, file ops, LLM)
  - **`types/`** - Agent type definitions
  - **`utils/`** - Agent utilities

- **`scripts/`** - Build and test automation
  - `unit.sh`, `integration.sh`, `contract.sh` - Test runners
  - `all-tests.sh` - Complete test suite + linting
  - `install.sh` / `install.ps1` - Build scripts
  - `binaries.sh` - Multi-platform build
  - `shipit.sh` - Release automation

- **`test/`** - Test fixtures and test-specific code
  - `mcp/` - MCP test servers (HTTP, SSE, STDIO)

- **`vendor/`** - Vendored dependencies (DO NOT EDIT MANUALLY)

### Configuration System

The application uses a 4-tier configuration hierarchy:

1. **Command-line flags** (highest priority)
2. **Environment variables** (prefixed by `name` field, e.g., `OPENAI_API_KEY`)
3. **Config file** (`~/.chatgpt-cli/config.yaml` or `config.<target>.yaml`)
4. **Default values** (lowest priority)

#### Custom Directories
Override defaults with environment variables:
- `OPENAI_CONFIG_HOME` - Config directory (default: `~/.chatgpt-cli`)
- `OPENAI_DATA_HOME` - History directory (default: `~/.chatgpt-cli/history`)
- `OPENAI_CACHE_HOME` - Cache directory (default: `~/.chatgpt-cli/cache`)

#### Multi-Provider Support
Use `--target` flag to switch between provider configs:
```bash
chatgpt --target perplexity "query"  # Uses config.perplexity.yaml
chatgpt --target azure "query"       # Uses config.azure.yaml
chatgpt "query"                      # Uses config.yaml (default)
```

### Agent Mode Architecture

The agent system supports two modes:

1. **ReAct Mode** (`--agent --agent-mode react`) - Default iterative loop
   - Think → Act → Observe cycle
   - LLM decides next action based on observations
   - Suitable for exploratory tasks

2. **Plan/Execute Mode** (`--agent --agent-mode plan`)
   - Generates complete plan first
   - Executes steps sequentially
   - Better for well-defined tasks

#### Agent Safety Model

**Budget Limits** (enforced by `agent/core/budget.go`):
- `max_iterations` - Max ReAct iterations
- `max_steps` - Max plan steps
- `max_wall_time` - Maximum execution time
- `max_shell_calls`, `max_llm_calls`, `max_file_ops` - Tool-specific limits
- `max_llm_tokens` - Token usage limit

**Policy Rules** (enforced by `agent/core/policy.go`):
- `allowed_tools` - Whitelist of tools (`[shell, llm, files]`)
- `denied_shell_commands` - Blacklist of shell commands (e.g., `[rm, sudo, dd]`)
- `allowed_file_ops` - File operation whitelist (`[read, write]`)
- `restrict_files_to_work_dir` - Sandbox file access to working directory

**Agent Logs:**
All agent runs write detailed logs to `$OPENAI_CACHE_HOME/agent/<timestamp>/`

### Thread-Based Context Management

Each conversation thread maintains its own history file:
- History stored in `$OPENAI_DATA_HOME/<thread>.json`
- Default thread: `default.json`
- Sliding window: automatically trims to `context_window` size
- Use `--thread <name>` to switch threads
- Use `--clear-history` to reset a thread

## Testing Conventions

### Test Structure
This project uses **sclevine/spec** with **gomega** assertions.

Example test pattern:
```go
func TestUnitFeature(t *testing.T) {
	spec.Run(t, "Testing the feature", testFeature, spec.Report(report.Terminal{}))
}

func testFeature(t *testing.T, when spec.G, it spec.S) {
	it.Before(func() {
		RegisterTestingT(t)
	})

	when("scenario description", func() {
		it("specific behavior", func() {
			// Arrange
			subject := NewSubject()

			// Act
			result, err := subject.DoSomething()

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(expected))
		})
	})
}
```

### Test Coverage Requirements
For any behavior change or new feature:
1. **Happy path** - Primary success scenario
2. **Failure path** - Main error/validation cases
3. **Invariant** - One critical invariant (e.g., no partial updates on failure)

### Mocking
- Use `//go:generate go run github.com/golang/mock/mockgen` for interfaces
- Mock files: `*mocks_test.go`
- Keep mocks in test files, not production code

### Running Tests
```bash
CONFIG_PATH="file://$PWD" TESTING=true go test -mod=vendor ./... -v -run Unit
CONFIG_PATH="file://$PWD" TESTING=true go test -mod=vendor ./... -v -run Integration
```

Or use Make targets (preferred):
```bash
make unit
make integration
make all-tests  # Includes lint, format check, TODO check
```

## Platform Notes (Windows)

The test suite and the CLI both have Windows-specific behaviour that does not
reproduce on Linux or macOS. All three of the following were real defects fixed
on 2026-09-19; the notes remain because the underlying platform differences do
not go away.

- **`os.UserHomeDir()` reads `USERPROFILE` on Windows, not `HOME`.** A test that
  sandboxes only `HOME` does not sandbox the spawned binary, which will write to
  the developer's real `~/.chatgpt-cli`. The integration suite now sets and
  restores both.
- **A second listener can bind a port another process already holds.** Neither
  side sets `SO_EXCLUSIVEADDRUSE`, so both then race for incoming connections and
  requests fail intermittently with "An existing connection was forcibly closed
  by the remote host". Never hardcode a port in tests: `net.Listen("tcp",
  "127.0.0.1:0")` and derive the URL from `listener.Addr()`.
- **`os.Rename` onto a path with an open handle fails with "Access is denied".**
  It succeeds silently on POSIX. Any atomic write (write temp, then rename) must
  close the destination *before* the rename, not in a `defer` that runs later.
- Windows cannot reliably use `0.0.0.0` as a *destination* address; bind on it
  if you like, but connect via loopback.
- `gofmt`/`golangci-lint` flag every file if the working tree is checked out
  CRLF. Compare formatting on LF-normalized content before believing a diff.

## Model Defaults

The default model is **`gpt-5.5`** (`config/store.go`, `cmd/chatgpt/main.go`).
Anything matching `gpt-5` routes through the **Responses API** (`/v1/responses`),
not `/v1/chat/completions` - see `GetCapabilities` in `api/client/llm.go:360`. The
integration mock server therefore registers handlers for both endpoints.

⚠️ **The `/v1/models` listing is stale and lists retired models.** `--list-models`
showed `gpt-5.2-codex` long after it began returning HTTP 404 on use. Never treat
the listing as proof of access; confirm a model with an actual query.

## Code Conventions

### Error Handling
- Use typed errors for recoverable conditions
- Check errors with `errors.As()` for typed errors
- Provide clear, actionable error messages

### Logging
- Use structured logging via `go.uber.org/zap`
- Log levels: Debug, Info, Warn, Error
- Agent logs automatically written to cache directory

### Configuration
- All config keys defined in `config/config.go`
- Use Viper for config loading/merging
- Support environment variable overrides with `name` prefix
- Provide `--set-*` flags for common config changes

### Code Style
- Follow existing patterns in the codebase
- Keep functions small and focused
- Prefer clarity over cleverness
- Run `go fmt` before committing
- Run `golangci-lint run` to catch issues. **Its headline count is truncated:**
  `max-same-issues` defaults to 3, so repeated findings of the same shape are
  hidden. Before claiming the gate is clean, run
  `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0`.
  (A pass reporting "20 issues" hid 8 more of the same shape.)

## Important Guardrails

1. **Never commit secrets** - Use environment variables and config files
2. **Do not modify `vendor/` manually** - Use `make updatedeps`
3. **Do not skip integration tests** - They catch real-world issues
4. **Do not edit `.git/`** - Standard Git operations only
5. **Treat runtime directories as read-only** - Don't modify `cache/`, `history/` directories
6. **Always run `go mod tidy` through Make** - Use `make updatedeps` to ensure consistency

## Common Development Workflows

### Adding a New CLI Flag
1. Add flag definition in `cmd/chatgpt/main.go` (rootCmd.Flags() section)
2. Add config field in `config/config.go`
3. Add Viper binding in `cmd/chatgpt/main.go` (viper.BindPFlag)
4. Update default config if needed
5. Add tests for the new behavior
6. Update relevant documentation

### Adding a New Agent Tool
1. Define tool interface in `agent/tools/`
2. Implement tool with policy checks
3. Update policy validation in `agent/core/policy.go`
4. Add budget tracking if needed
5. Write unit tests with mocks
6. Write integration test with real tool
7. Update AGENTS.md if necessary

### Adding a New LLM Provider
1. Create config template in `config/` (see Azure/Perplexity examples)
2. Update environment variable prefix handling
3. Test with `--target` flag
4. Document provider-specific configuration
5. Add integration test if possible

### Debugging Agent Runs
Agent logs are written to `$OPENAI_CACHE_HOME/agent/<timestamp>/`:
- Check planner output (Plan/Execute mode)
- Review tool calls and results
- Inspect budget usage
- Enable debug logging with `--set-debug true`
