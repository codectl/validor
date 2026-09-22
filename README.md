# validor

Go package ensuring reliable terraform deployments

## Why validor?

Terraform modules can fail in production due to untested configurations, provider incompatibilities, or incomplete setups.

Manual testing is time-consuming and error-prone.

Validor helps you:

Test modules in isolated environments before production.

Validate apply/destroy cycles with real provider interactions.

Run multiple modules concurrently for faster CI/CD pipelines.

Test with local sources, exceptions, and custom configurations.

Automate testing across teams and large codebases.

## Installation

`go get github.com/codectl/validor`

## Testing

Run all tests (requires Terraform):

`go test ./...`

Run tests with coverage:

`go test -coverprofile=coverage.out ./...`

`go tool cover -html=coverage.out`

Tests run against the fixture module (no cloud credentials or provider downloads required) and validate both unit functionality and end-to-end public api behavior.

## Features

`Module Testing`

Executes full Terraform apply/destroy cycles for real validation.

Supports parallel and sequential execution modes.

Handles local source testing for module development.

Provides detailed error reporting with actionable feedback.

`Flexible Configuration`

Command-line flags for runtime configuration (`-example`, `-exception`, `-local`, `-namespace`, `-examples-path`).

Optional pattern support for programmatic configuration (e.g., `WithExamplesPath`, `WithExample`).

Environment variable support for CI/CD integration.

Exception lists to skip problematic modules.

Configurable namespace for custom registry sources.

Customizable examples directory path for flexible project structures.

`Advanced Terraform Support`

Works with all major cloud providers and custom modules.

Respects Terraform state and resource dependencies.

Handles complex module structures and submodules.

Automatic cleanup of generated files and states.

`Error Reporting & Logging`

Structured error types for better debugging.

Outputs test summaries with failure details.

Integration with Go testing framework for CI/CD.

## Configuration

### Command-Line Flags

`-example`: Comma-separated list of specific examples to test.

`-exception`: Comma-separated list of examples to exclude.

`-local`: Use local source paths instead of registry.

`-namespace`: Terraform registry namespace (default: "codectl").

`-skip-destroy`: Skip destroy operations after apply.

`-examples-path`: Path to examples directory (defaults to '../examples').

## Contributors

We welcome contributions from the community! Whether it's reporting a bug, suggesting a new feature, or submitting a pull request, your input is highly valued. <br><br>
