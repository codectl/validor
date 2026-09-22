package validor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var fixtureFiles = map[string]string{
	"main.tf": `resource "terraform_data" "this" {
  input = var.name
}
`,
	"variables.tf": `variable "name" {
  type = string
}
`,
	"outputs.tf": `output "name" {
  value = terraform_data.this.output
}
`,
	"examples/default/main.tf": `module "example" {
  source = "../../"

  name = "default"
}
`,
	"examples/complete/main.tf": `module "example" {
  source = "../../"

  name = "complete"
}

output "name" {
  value = module.example.name
}
`,
}

func copyFixture(t *testing.T) (repo, examples string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "terraform-azure-example")
	for name, content := range fixtureFiles {
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo, filepath.Join(repo, "examples")
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

type fakeRegistry struct {
	version string
	err     error
}

func (f *fakeRegistry) GetLatestVersion(context.Context, string, string, string) (string, error) {
	return f.version, f.err
}

func TestApplyEntryPoints(t *testing.T) {
	tests := []struct {
		name      string
		run       func(t *testing.T, examples string)
		wantState map[string]bool // example -> tfstate left behind
	}{
		{
			name: "TestApplyNoError single example",
			run: func(t *testing.T, examples string) {
				TestApplyNoError(t, WithExamplesPath(examples), WithExample("default"), WithSkipDestroy(true))
			},
			wantState: map[string]bool{"default": true, "complete": false},
		},
		{
			name: "TestApplyNoError multiple examples",
			run: func(t *testing.T, examples string) {
				TestApplyNoError(t, WithExamplesPath(examples), WithExample("default,complete"), WithSkipDestroy(true))
			},
			wantState: map[string]bool{"default": true, "complete": true},
		},
		{
			name: "TestApplyAllParallel destroys and cleans up",
			run: func(t *testing.T, examples string) {
				TestApplyAllParallel(t, WithExamplesPath(examples))
			},
			wantState: map[string]bool{"default": false, "complete": false},
		},
		{
			name: "TestApplyAllSequential honours exception",
			run: func(t *testing.T, examples string) {
				TestApplyAllSequential(t, WithExamplesPath(examples), WithException("complete"), WithSkipDestroy(true))
			},
			wantState: map[string]bool{"default": true, "complete": false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, examples := copyFixture(t)

			t.Run("run", func(t *testing.T) { tt.run(t, examples) })

			for example, want := range tt.wantState {
				if got := exists(t, filepath.Join(examples, example, "terraform.tfstate")); got != want {
					t.Errorf("%s: tfstate present = %v, want %v", example, got, want)
				}
			}
		})
	}
}

func TestApplyAllLocalRewritesSources(t *testing.T) {
	repo, examples := copyFixture(t)
	registrySource := `module "example" {
  source  = "codectl/example/azure"
  version = "~> 1.0"

  name = "default"
}
`
	mainTF := filepath.Join(examples, "default", "main.tf")
	if err := os.WriteFile(mainTF, []byte(registrySource), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	orig := newRegistryClient
	t.Cleanup(func() { newRegistryClient = orig })
	newRegistryClient = func() RegistryClient { return &fakeRegistry{version: "2.3.0"} }

	t.Run("run", func(t *testing.T) {
		TestApplyAllLocal(t, WithExamplesPath(examples), WithException("complete"))
	})

	got, err := os.ReadFile(mainTF)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(registrySource, `"~> 1.0"`, `"~> 2.3.0"`, 1)
	if string(got) != want {
		t.Fatalf("restored file:\n%s\nwant:\n%s", got, want)
	}
	if exists(t, filepath.Join(examples, "default", "terraform.tfstate")) {
		t.Fatal("state left behind after destroy")
	}
}

func TestConvertModulesToLocal(t *testing.T) {
	registrySource := `module "test" {
  source  = "codectl/mymodule/azure"
  version = "~> 1.0"
}
`
	info := ModuleInfo{Name: "mymodule", Provider: "azure", Namespace: "codectl"}

	tests := []struct {
		name       string
		modules    []string
		exceptions []string
		cancelled  bool
		want       int
	}{
		{"every module converted", []string{"example1", "example2"}, nil, false, 2},
		{"exception skipped", []string{"example1", "example2"}, []string{"example2"}, false, 1},
		{"cancelled context converts nothing", []string{"example1"}, nil, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			examples := t.TempDir()
			for _, name := range tt.modules {
				dir := filepath.Join(examples, name)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(registrySource), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			ctx := context.Background()
			if tt.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			converter := NewSourceConverter(&fakeRegistry{version: "1.0.0"})
			restores := convertModulesToLocal(ctx, t, converter, tt.modules, tt.exceptions, info, examples)
			if len(restores) != tt.want {
				t.Fatalf("got %d restores, want %d", len(restores), tt.want)
			}
			for _, r := range restores {
				if r.OriginalContent != registrySource {
					t.Fatalf("restore lost original content: %q", r.OriginalContent)
				}
			}
		})
	}
}

func TestNewSourceConverterAndRegistryClient(t *testing.T) {
	if _, ok := NewSourceConverter(NewRegistryClient()).(*DefaultSourceConverter); !ok {
		t.Fatal("NewSourceConverter must return *DefaultSourceConverter")
	}
	if _, ok := NewRegistryClient().(*DefaultRegistryClient); !ok {
		t.Fatal("NewRegistryClient must return *DefaultRegistryClient")
	}
}

func TestBoolToStr(t *testing.T) {
	tests := []struct {
		name string
		cond bool
		want string
	}{
		{"true picks yes", true, "yes"},
		{"false picks no", false, "no"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BoolToStr(tt.cond, "yes", "no"); got != tt.want {
				t.Fatalf("BoolToStr() = %q, want %q", got, tt.want)
			}
		})
	}
}
