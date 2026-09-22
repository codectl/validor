package validor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/gruntwork-io/terratest/modules/terraform"
)

var cleanupPatterns = []string{"*.terraform*", "*tfstate*", "*.lock.hcl"}

// Module is one example directory under test.
type Module struct {
	Name        string
	Path        string
	Options     *terraform.Options
	Errors      []error
	ApplyFailed bool

	applyHook   func(ctx context.Context, t *testing.T, m *Module) error
	destroyHook func(ctx context.Context, t *testing.T, m *Module) error
	cleanupHook func(ctx context.Context, t *testing.T, m *Module) error
}

type testLogger interface {
	Helper()
	Log(args ...any)
	Logf(format string, args ...any)
	Fatal(args ...any)
}

// ModuleManager discovers example modules under an examples directory.
type ModuleManager struct {
	BaseExamplesPath string
	Config           *Config
}

func NewModuleManager(baseExamplesPath string) *ModuleManager {
	return &ModuleManager{
		BaseExamplesPath: baseExamplesPath,
	}
}

func (mm *ModuleManager) SetConfig(config *Config) {
	mm.Config = config
}

func NewModule(name, path string) *Module {
	return &Module{
		Name: name,
		Path: path,
		Options: &terraform.Options{
			TerraformDir:    path,
			NoColor:         true,
			TerraformBinary: "terraform",
		},
	}
}

// DiscoverModules returns one Module per directory under BaseExamplesPath,
// skipping those on the configured exception list.
func (mm *ModuleManager) DiscoverModules() ([]*Module, error) {
	var modules []*Module

	entries, err := os.ReadDir(mm.BaseExamplesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read examples directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			moduleName := entry.Name()
			if mm.Config != nil && slices.Contains(mm.Config.ExceptionList, moduleName) {
				fmt.Printf("Skipping module %s as it is in the exception list\n", moduleName)
				continue
			}
			modulePath := filepath.Join(mm.BaseExamplesPath, moduleName)
			modules = append(modules, NewModule(moduleName, modulePath))
		}
	}

	return modules, nil
}

func (m *Module) Apply(ctx context.Context, t *testing.T) error {
	t.Helper()

	if m.applyHook != nil {
		return m.applyHook(ctx, t, m)
	}

	t.Logf("Applying Terraform module: %s", m.Name)
	m.Options = terraform.WithDefaultRetryableErrors(t, m.Options)

	_, err := terraform.InitAndApplyE(t, m.Options)
	if err != nil {
		m.ApplyFailed = true
		return m.recordError(t, "terraform apply", err)
	}
	return nil
}

func (m *Module) Destroy(ctx context.Context, t *testing.T) error {
	t.Helper()

	if m.destroyHook != nil {
		destroyErr := m.destroyHook(ctx, t, m)
		m.recordErrorIfAllowed(t, "terraform destroy", destroyErr)

		if m.cleanupHook != nil && !m.ApplyFailed {
			m.recordErrorIfAllowed(t, "cleanup", m.cleanupHook(ctx, t, m))
		}
		return destroyErr
	}

	t.Logf("Destroying Terraform module: %s", m.Name)

	_, destroyErr := terraform.DestroyE(t, m.Options)
	m.recordErrorIfAllowed(t, "terraform destroy", destroyErr)
	m.recordErrorIfAllowed(t, "cleanup", m.Cleanup(ctx, t))

	return destroyErr
}

// Cleanup removes terraform state, lock and plugin files from the module
// directory.
func (m *Module) Cleanup(ctx context.Context, t *testing.T) error {
	t.Helper()

	if m.cleanupHook != nil {
		return m.cleanupHook(ctx, t, m)
	}

	t.Logf("Cleaning up in: %s", m.Options.TerraformDir)
	for _, pattern := range cleanupPatterns {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		matches, err := filepath.Glob(filepath.Join(m.Options.TerraformDir, pattern))
		if err != nil {
			return fmt.Errorf("error matching pattern %s: %w", pattern, err)
		}
		for _, filePath := range matches {
			if err := os.RemoveAll(filePath); err != nil {
				return fmt.Errorf("failed to remove %s: %w", filePath, err)
			}
		}
	}
	return nil
}

func (m *Module) recordErrorIfAllowed(t *testing.T, operation string, err error) error {
	t.Helper()

	if err == nil || m.ApplyFailed {
		return err
	}
	return m.recordError(t, operation, err)
}

func (m *Module) recordError(t *testing.T, operation string, err error) error {
	t.Helper()

	wrappedErr := &ModuleError{ModuleName: m.Name, Operation: operation, Err: err}
	m.Errors = append(m.Errors, wrappedErr)
	t.Log(redError(wrappedErr.Error()))
	return wrappedErr
}

// PrintModuleSummary logs every failed module with its errors and a
// pass/fail total.
func PrintModuleSummary(tb testLogger, modules []*Module) {
	tb.Helper()

	var failedModules []*Module
	for _, module := range modules {
		if len(module.Errors) > 0 {
			failedModules = append(failedModules, module)
		}
	}

	if len(failedModules) > 0 {
		for _, module := range failedModules {
			tb.Log(redError("Module " + module.Name + " failed with errors:"))
			for i, err := range module.Errors {
				errText := fmt.Sprintf("  %d. %v", i+1, err)
				tb.Log(redError(errText))
			}
			tb.Log("")
		}

		totalText := fmt.Sprintf("TOTAL: %d of %d modules failed", len(failedModules), len(modules))
		tb.Log(redError(totalText))
	} else {
		tb.Logf("\n==== SUCCESS: All %d modules applied and destroyed successfully ====", len(modules))
	}
}

// ModuleError wraps a failed operation on a module.
type ModuleError struct {
	ModuleName string
	Operation  string
	Err        error
}

func (e *ModuleError) Error() string {
	return fmt.Sprintf("%s failed for module %s: %v", e.Operation, e.ModuleName, e.Err)
}

func (e *ModuleError) Unwrap() error {
	return e.Err
}

// TestResults collects finished modules from concurrently running subtests.
type TestResults struct {
	mu            sync.RWMutex
	modules       []*Module
	failedModules []*Module
}

func NewTestResults() *TestResults {
	return &TestResults{}
}

func (tr *TestResults) AddModule(module *Module) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.modules = append(tr.modules, module)
	if len(module.Errors) > 0 {
		tr.failedModules = append(tr.failedModules, module)
	}
}

func (tr *TestResults) GetResults() ([]*Module, []*Module) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.modules, tr.failedModules
}
