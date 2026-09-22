// Package validor validates Terraform modules by applying and destroying
// their examples with the real provider.
//
// A module repository wires validor up as Go tests in its tests directory,
// one per mode: TestApplyNoError applies the examples named by -example,
// TestApplyAllParallel and TestApplyAllSequential apply every directory
// under ../examples, and TestApplyAllLocal does the same after pointing the
// module sources of the examples at the checkout instead of the registry.
// Flags (-example, -exception, -local, -skip-destroy, -namespace,
// -examples-path) drive the run from the command line; Options do the same
// from code and, when given, replace the flags entirely.
package validor

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/fatih/color"
)

// Logger is the minimal test logger contract used by runner abstractions.
type Logger interface {
	Helper()
	Logf(format string, args ...any)
	Log(args ...any)
}

// ModuleRunner applies, destroys, and cleans up a Terraform module.
type ModuleRunner interface {
	Apply(ctx context.Context, logger Logger) error
	Destroy(ctx context.Context, logger Logger) error
	Cleanup(ctx context.Context, logger Logger) error
}

// ModuleDiscoverer discovers Terraform example modules from a configured source.
type ModuleDiscoverer interface {
	DiscoverModules() ([]*Module, error)
	SetConfig(config *Config)
}

// SourceConverter converts Terraform module sources between registry and local paths.
type SourceConverter interface {
	ConvertToLocal(ctx context.Context, modulePath string, moduleInfo ModuleInfo) ([]FileRestore, error)
	RevertToRegistry(ctx context.Context, filesToRestore []FileRestore) error
}

// RegistryClient fetches Terraform Registry metadata.
type RegistryClient interface {
	GetLatestVersion(ctx context.Context, namespace, name, provider string) (string, error)
}

// TestRunner runs Terraform module tests.
type TestRunner interface {
	RunTests(logger Logger, modules []*Module, parallel bool, config *Config)
	RunLocalTests(logger Logger, examplesPath string) error
}

// TestApplyNoError applies the examples named by -example (or WithExample).
func TestApplyNoError(t *testing.T, opts ...Option) {
	config := setupConfigWithOptions(opts...)
	if config.Example == "" {
		t.Fatal(redError("-example flag is not set"))
	}
	modules := createModulesFromNames(parseExampleList(config.Example), getExamplesPath(config))
	sourceType := BoolToStr(config.Local, "local", "registry")
	var setup TestSetupFunc
	if config.Local {
		setup = createLocalSetupFunc(config)
	}
	runModuleTests(t, modules, true, config, setup, sourceType)
}

// TestApplyAllParallel applies every discovered example concurrently.
func TestApplyAllParallel(t *testing.T, opts ...Option) {
	config := setupConfigWithOptions(opts...)
	modules := discoverModules(t, config)
	RunTests(t, modules, true, config)
}

// TestApplyAllSequential applies every discovered example one after another.
func TestApplyAllSequential(t *testing.T, opts ...Option) {
	config := setupConfigWithOptions(opts...)
	modules := discoverModules(t, config)
	RunTests(t, modules, false, config)
}

// TestApplyAllLocal applies every discovered example concurrently with its
// module sources pointed at the local checkout.
func TestApplyAllLocal(t *testing.T, opts ...Option) {
	config := setupConfigWithOptions(opts...)
	modules := discoverModules(t, config)
	runModuleTests(t, modules, true, config, createLocalSetupFunc(config), "local")
}

// RunTests applies and destroys modules from their registry sources.
func RunTests(t *testing.T, modules []*Module, parallel bool, config *Config) {
	runModuleTests(t, modules, parallel, config, nil, "registry")
}

// RunTestsWithOptions applies and destroys the modules selected by opts.
func RunTestsWithOptions(t *testing.T, opts ...TestOption) {
	tc := &TestConfig{
		Parallel: true,
	}

	for _, opt := range opts {
		opt(tc)
	}

	if tc.Config == nil {
		tc.Config = NewConfig()
	}

	if tc.ExamplesPath != "" {
		tc.Config.ExamplesPath = tc.ExamplesPath
	}

	modules := createModulesFromNames(tc.ModuleNames, getExamplesPath(tc.Config))
	sourceType := BoolToStr(tc.UseLocal, "local", "registry")
	var setup TestSetupFunc
	if tc.UseLocal {
		setup = createLocalSetupFunc(tc.Config)
	}
	runModuleTestsFn(t, modules, tc.Parallel, tc.Config, setup, sourceType)
}

func runModuleTests(t *testing.T, modules []*Module, parallel bool, config *Config, setup TestSetupFunc, sourceType string) {
	ctx := context.Background()
	results := NewTestResults()

	if setup != nil {
		if err := setup(ctx, t, modules); err != nil {
			t.Fatal(redError(fmt.Sprintf("Setup failed: %v", err)))
		}
	}

	for _, module := range modules {
		if slices.Contains(config.ExceptionList, module.Name) {
			t.Logf("Skipping example %s as it is in the exception list", module.Name)
			continue
		}

		t.Run(module.Name, func(t *testing.T) {
			if parallel {
				t.Parallel()
			}

			if err := module.Apply(ctx, t); err != nil {
				t.Fail()
			} else {
				t.Logf("✓ Module %s applied successfully with %s source", module.Name, sourceType)
			}

			if !config.SkipDestroy {
				if err := module.Destroy(ctx, t); err != nil && !module.ApplyFailed {
					t.Logf("Cleanup failed for module %s: %v", module.Name, err)
				}
			}

			results.AddModule(module)
		})
	}

	t.Cleanup(func() {
		modules, _ := results.GetResults()
		PrintModuleSummary(t, modules)
	})
}

func discoverModules(t *testing.T, config *Config) []*Module {
	examplesPath := getExamplesPath(config)
	manager := NewModuleManager(examplesPath)
	manager.SetConfig(config)
	modules, err := manager.DiscoverModules()
	if err != nil {
		errText := fmt.Sprintf("Failed to discover modules: %v", err)
		t.Fatal(redError(errText))
	}
	return modules
}

func extractModuleNames(modules []*Module) []string {
	moduleNames := make([]string, 0, len(modules))
	for _, module := range modules {
		moduleNames = append(moduleNames, module.Name)
	}
	return moduleNames
}

func createModulesFromNames(moduleNames []string, basePath string) []*Module {
	modules := make([]*Module, 0, len(moduleNames))
	for _, name := range moduleNames {
		path := filepath.Join(basePath, name)
		modules = append(modules, NewModule(name, path))
	}
	return modules
}

var runModuleTestsFn = runModuleTests

var redError = color.New(color.FgHiRed, color.Bold).SprintFunc()

// BoolToStr returns yes when cond holds, no otherwise.
func BoolToStr(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}
