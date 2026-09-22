package validor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

type mockTB struct {
	logs []string
}

func (m *mockTB) Helper() {}

func (m *mockTB) Log(args ...any) {
	m.logs = append(m.logs, strings.TrimSpace(fmt.Sprint(args...)))
}

func (m *mockTB) Logf(format string, args ...any) {
	m.logs = append(m.logs, fmt.Sprintf(format, args...))
}

func (m *mockTB) Fatal(args ...any) {
	m.logs = append(m.logs, strings.TrimSpace(fmt.Sprint(args...)))
}

func hook(err error, called *bool) func(context.Context, *testing.T, *Module) error {
	return func(context.Context, *testing.T, *Module) error {
		if called != nil {
			*called = true
		}
		return err
	}
}

func failed(name string, errs ...error) *Module {
	return &Module{Name: name, Path: "/path/" + name, Errors: errs}
}

func TestNewModule(t *testing.T) {
	m := NewModule("example1", "/path/to/example1")
	if m.Name != "example1" || m.Path != "/path/to/example1" {
		t.Fatalf("Name/Path = %q/%q", m.Name, m.Path)
	}
	if m.Options.TerraformDir != m.Path || !m.Options.NoColor || m.Options.TerraformBinary != "terraform" {
		t.Fatalf("unexpected terraform options %+v", m.Options)
	}
	if m.ApplyFailed || len(m.Errors) != 0 {
		t.Fatalf("new module must start clean, got failed=%v errors=%v", m.ApplyFailed, m.Errors)
	}
}

func TestModuleManagerDiscoverModules(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"example1", "example2", "example3"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		config  *Config
		want    []string
		wantErr bool
	}{
		{"directories only", root, &Config{}, []string{"example1", "example2", "example3"}, false},
		{"nil config", root, nil, []string{"example1", "example2", "example3"}, false},
		{"exception list applied", root, &Config{ExceptionList: []string{"example2"}}, []string{"example1", "example3"}, false},
		{"missing directory", filepath.Join(root, "nope"), &Config{}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := NewModuleManager(tt.path)
			mm.SetConfig(tt.config)

			modules, err := mm.DiscoverModules()
			if (err != nil) != tt.wantErr {
				t.Fatalf("DiscoverModules() error = %v, wantErr %v", err, tt.wantErr)
			}
			var got []string
			for _, m := range modules {
				got = append(got, m.Name)
				if m.Path != filepath.Join(tt.path, m.Name) {
					t.Errorf("module %s path = %q", m.Name, m.Path)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("discovered %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModuleNames(t *testing.T) {
	tests := []struct {
		name  string
		names []string
	}{
		{"empty", []string{}},
		{"single", []string{"example1"}},
		{"multiple", []string{"example1", "example2", "example3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modules := createModulesFromNames(tt.names, "/base")
			for i, m := range modules {
				if m.Path != filepath.Join("/base", tt.names[i]) {
					t.Errorf("module %s path = %q", m.Name, m.Path)
				}
			}
			if got := extractModuleNames(modules); !reflect.DeepEqual(got, tt.names) {
				t.Fatalf("round trip = %v, want %v", got, tt.names)
			}
		})
	}
}

func TestModuleCleanup(t *testing.T) {
	tests := []struct {
		file    string
		removed bool
	}{
		{".terraform", true},
		{"terraform.tfstate", true},
		{"terraform.tfstate.backup", true},
		{".terraform.lock.hcl", true},
		{"main.tf", false},
		{"README.md", false},
	}

	dir := t.TempDir()
	for _, tt := range tests {
		if err := os.WriteFile(filepath.Join(dir, tt.file), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := NewModule("test", dir).Cleanup(context.Background(), t); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			_, err := os.Stat(filepath.Join(dir, tt.file))
			if gone := os.IsNotExist(err); gone != tt.removed {
				t.Fatalf("removed = %v, want %v", gone, tt.removed)
			}
		})
	}

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := NewModule("test", dir).Cleanup(ctx, t); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	})
}

func TestModuleDestroy(t *testing.T) {
	destroyErr := errors.New("destroy failed")
	cleanupErr := errors.New("cleanup failed")

	tests := []struct {
		name        string
		applyFailed bool
		destroy     error
		cleanup     error
		wantErr     error
		wantErrors  int
		wantCleanup bool
	}{
		{"clean run", false, nil, nil, nil, 0, true},
		{"destroy and cleanup errors both recorded", false, destroyErr, cleanupErr, destroyErr, 2, true},
		{"cleanup skipped and errors dropped after failed apply", true, destroyErr, cleanupErr, destroyErr, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cleanupCalled bool
			m := NewModule("test", t.TempDir())
			m.ApplyFailed = tt.applyFailed
			m.destroyHook = hook(tt.destroy, nil)
			m.cleanupHook = hook(tt.cleanup, &cleanupCalled)

			if err := m.Destroy(context.Background(), t); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Destroy() error = %v, want %v", err, tt.wantErr)
			}
			if len(m.Errors) != tt.wantErrors {
				t.Fatalf("recorded %d errors, want %d: %v", len(m.Errors), tt.wantErrors, m.Errors)
			}
			if cleanupCalled != tt.wantCleanup {
				t.Fatalf("cleanup called = %v, want %v", cleanupCalled, tt.wantCleanup)
			}
			for _, err := range m.Errors {
				var me *ModuleError
				if !errors.As(err, &me) || me.ModuleName != "test" {
					t.Fatalf("recorded error %v is not a ModuleError for this module", err)
				}
			}
		})
	}
}

func TestRunModuleTests(t *testing.T) {
	tests := []struct {
		name        string
		parallel    bool
		config      *Config
		modules     []string
		wantApplied []string
		wantDestroy bool
	}{
		{"sequential applies and destroys", false, &Config{}, []string{"mod1"}, []string{"mod1"}, true},
		{"parallel applies and destroys", true, &Config{}, []string{"mod1", "mod2"}, []string{"mod1", "mod2"}, true},
		{"skip destroy", false, &Config{SkipDestroy: true}, []string{"mod1"}, []string{"mod1"}, false},
		{"exception list skips module", false, &Config{ExceptionList: []string{"skip-me"}}, []string{"run-me", "skip-me"}, []string{"run-me"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var applied []string
			var destroyed bool

			modules := createModulesFromNames(tt.modules, t.TempDir())
			for _, m := range modules {
				m.applyHook = func(_ context.Context, _ *testing.T, m *Module) error {
					mu.Lock()
					defer mu.Unlock()
					applied = append(applied, m.Name)
					return nil
				}
				m.destroyHook = func(context.Context, *testing.T, *Module) error {
					mu.Lock()
					defer mu.Unlock()
					destroyed = true
					return nil
				}
			}

			// Subtests, parallel ones included, finish before Run returns.
			t.Run("run", func(t *testing.T) {
				runModuleTests(t, modules, tt.parallel, tt.config, nil, "registry")
			})

			mu.Lock()
			defer mu.Unlock()
			slices.Sort(applied)
			if !reflect.DeepEqual(applied, tt.wantApplied) {
				t.Fatalf("applied %v, want %v", applied, tt.wantApplied)
			}
			if destroyed != tt.wantDestroy {
				t.Fatalf("destroy called = %v, want %v", destroyed, tt.wantDestroy)
			}
		})
	}
}

func TestRunTestsWithOptions(t *testing.T) {
	orig := runModuleTestsFn
	t.Cleanup(func() { runModuleTestsFn = orig })

	tests := []struct {
		name         string
		opts         []TestOption
		wantParallel bool
		wantPath     string
		wantSource   string
		wantModules  []string
	}{
		{"defaults", []TestOption{WithModules([]string{"a"})}, true, "../examples", "registry", []string{"a"}},
		{"overrides", []TestOption{WithTestExamplesPath("/tmp/examples"), WithParallel(false), WithModules([]string{"a", "b"})}, false, "/tmp/examples", "registry", []string{"a", "b"}},
		{"config supplied", []TestOption{WithConfig(NewConfig(WithExamplesPath("/cfg"))), WithModules([]string{"a"})}, true, "/cfg", "registry", []string{"a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			runModuleTestsFn = func(_ *testing.T, modules []*Module, parallel bool, config *Config, setup TestSetupFunc, sourceType string) {
				called = true
				if parallel != tt.wantParallel {
					t.Errorf("parallel = %v, want %v", parallel, tt.wantParallel)
				}
				if got := getExamplesPath(config); got != tt.wantPath {
					t.Errorf("examples path = %q, want %q", got, tt.wantPath)
				}
				if sourceType != tt.wantSource || setup != nil {
					t.Errorf("source = %q setup=%v, want %q without setup", sourceType, setup != nil, tt.wantSource)
				}
				if got := extractModuleNames(modules); !reflect.DeepEqual(got, tt.wantModules) {
					t.Errorf("modules = %v, want %v", got, tt.wantModules)
				}
			}

			RunTestsWithOptions(t, tt.opts...)
			if !called {
				t.Fatal("runModuleTests not invoked")
			}
		})
	}

	t.Run("local source installs setup", func(t *testing.T) {
		runModuleTestsFn = func(_ *testing.T, _ []*Module, _ bool, _ *Config, setup TestSetupFunc, sourceType string) {
			if setup == nil || sourceType != "local" {
				t.Errorf("setup=%v source=%q, want setup with local source", setup != nil, sourceType)
			}
		}
		RunTestsWithOptions(t, WithLocalSource(true), WithModules([]string{"a"}))
	})
}

func TestPrintModuleSummary(t *testing.T) {
	tests := []struct {
		name    string
		modules []*Module
		want    []string
	}{
		{
			name:    "all successful",
			modules: []*Module{NewModule("a", "/a"), NewModule("b", "/b"), NewModule("c", "/c")},
			want:    []string{"SUCCESS: All 3 modules"},
		},
		{
			name:    "one failed",
			modules: []*Module{NewModule("a", "/a"), failed("b", errors.New("terraform apply failed")), NewModule("c", "/c")},
			want:    []string{"Module b failed", "1. terraform apply failed", "TOTAL: 1 of 3 modules failed"},
		},
		{
			name:    "multiple errors count once",
			modules: []*Module{failed("a", errors.New("e1"), errors.New("e2"), errors.New("e3")), NewModule("b", "/b")},
			want:    []string{"1. e1", "2. e2", "3. e3", "TOTAL: 1 of 2 modules failed"},
		},
		{
			name:    "no modules",
			modules: nil,
			want:    []string{"SUCCESS: All 0 modules"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockTB{}
			PrintModuleSummary(mock, tt.modules)
			got := strings.Join(mock.logs, "\n")
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("summary missing %q:\n%s", want, got)
				}
			}
		})
	}
}

func TestTestResults(t *testing.T) {
	tests := []struct {
		name       string
		modules    []*Module
		wantTotal  int
		wantFailed int
	}{
		{"empty", nil, 0, 0},
		{"successful only", []*Module{NewModule("a", "/a")}, 1, 0},
		{"mixed", []*Module{NewModule("a", "/a"), failed("b", errors.New("x"))}, 2, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := NewTestResults()
			for _, m := range tt.modules {
				results.AddModule(m)
			}
			all, failedModules := results.GetResults()
			if len(all) != tt.wantTotal || len(failedModules) != tt.wantFailed {
				t.Fatalf("got %d/%d, want %d/%d", len(all), len(failedModules), tt.wantTotal, tt.wantFailed)
			}
		})
	}

	t.Run("concurrent adds", func(t *testing.T) {
		results := NewTestResults()
		var wg sync.WaitGroup
		for i := range 10 {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				m := NewModule("test", "/path")
				if id%2 == 0 {
					m.Errors = append(m.Errors, errors.New("error"))
				}
				results.AddModule(m)
			}(i)
		}
		wg.Wait()
		all, failedModules := results.GetResults()
		if len(all) != 10 || len(failedModules) != 5 {
			t.Fatalf("got %d/%d, want 10/5", len(all), len(failedModules))
		}
	})
}

func TestModuleError(t *testing.T) {
	tests := []struct {
		name      string
		module    string
		operation string
		err       error
		want      string
	}{
		{"apply", "test-module", "terraform apply", errors.New("resource not found"), "terraform apply failed for module test-module: resource not found"},
		{"destroy", "example-module", "terraform destroy", errors.New("timeout"), "terraform destroy failed for module example-module: timeout"},
		{"cleanup", "my-module", "cleanup", errors.New("permission denied"), "cleanup failed for module my-module: permission denied"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &ModuleError{ModuleName: tt.module, Operation: tt.operation, Err: tt.err}
			if got := err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
			if !errors.Is(err, tt.err) {
				t.Fatal("errors.Is must see the wrapped error")
			}
		})
	}
}
