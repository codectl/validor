package validor

import (
	"context"
	"testing"
)

// TestOption configures RunTestsWithOptions.
type TestOption func(*TestConfig)

// TestSetupFunc prepares modules before they are applied.
type TestSetupFunc func(ctx context.Context, t *testing.T, modules []*Module) error

type TestConfig struct {
	Config       *Config
	ModuleNames  []string
	UseLocal     bool
	Parallel     bool
	ExamplesPath string
}

func WithConfig(config *Config) TestOption {
	return func(tc *TestConfig) { tc.Config = config }
}

func WithModules(moduleNames []string) TestOption {
	return func(tc *TestConfig) { tc.ModuleNames = moduleNames }
}

func WithLocalSource(useLocal bool) TestOption {
	return func(tc *TestConfig) { tc.UseLocal = useLocal }
}

func WithParallel(parallel bool) TestOption {
	return func(tc *TestConfig) { tc.Parallel = parallel }
}

func WithTestExamplesPath(path string) TestOption {
	return func(tc *TestConfig) { tc.ExamplesPath = path }
}
