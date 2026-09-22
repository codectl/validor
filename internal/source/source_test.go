package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

type fakeRegistry struct {
	version string
	err     error
}

func (f *fakeRegistry) GetLatestVersion(context.Context, string, string, string) (string, error) {
	return f.version, f.err
}

var info = Info{Name: "mymodule", Provider: "azure", Namespace: "codectl"}

func writeTF(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "main.tf")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConvertToLocal(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantRestores int
		wantSources  []string
		wantVersion  bool
	}{
		{
			name: "root and submodule sources rewritten, version dropped",
			content: `module "test" {
  source  = "codectl/mymodule/azure"
  version = "~> 1.0"
}

module "submodule" {
  source  = "codectl/mymodule/azure//modules/network"
  version = "~> 1.0"
}
`,
			wantRestores: 1,
			wantSources:  []string{`"../../"`, `"../../modules/network"`},
		},
		{
			name: "foreign source untouched",
			content: `module "test" {
  source  = "hashicorp/consul/aws"
  version = "~> 1.0"
}
`,
			wantRestores: 0,
			wantSources:  []string{`"hashicorp/consul/aws"`},
			wantVersion:  true,
		},
		{
			name: "already local untouched",
			content: `module "test" {
  source = "../../"
}
`,
			wantRestores: 0,
			wantSources:  []string{`"../../"`},
		},
		{
			name: "nested module block rewritten",
			content: `locals {}

resource "null_resource" "wrap" {
  module "inner" {
    source  = "codectl/mymodule/azure"
    version = "~> 1.0"
  }
}
`,
			wantRestores: 1,
			wantSources:  []string{`"../../"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := writeTF(t, dir, tt.content)

			restores, err := NewConverter(&fakeRegistry{}).ConvertToLocal(context.Background(), dir, info)
			if err != nil {
				t.Fatalf("ConvertToLocal() error = %v", err)
			}
			if len(restores) != tt.wantRestores {
				t.Fatalf("got %d restores, want %d", len(restores), tt.wantRestores)
			}
			if tt.wantRestores == 1 && restores[0].OriginalContent != tt.content {
				t.Fatalf("restore did not capture original content")
			}

			got, _ := os.ReadFile(path)
			for _, src := range tt.wantSources {
				if !regexp.MustCompile(`source\s*=\s*` + regexp.QuoteMeta(src)).Match(got) {
					t.Errorf("expected source %s in:\n%s", src, got)
				}
			}
			if strings.Contains(string(got), "version") != tt.wantVersion {
				t.Errorf("version attribute present = %v, want %v:\n%s", !tt.wantVersion, tt.wantVersion, got)
			}
		})
	}
}

func TestConvertToLocalErrors(t *testing.T) {
	t.Run("cancelled context leaves files untouched", func(t *testing.T) {
		dir := t.TempDir()
		content := `module "one" {
  source  = "codectl/mymodule/azure"
  version = "~> 1.0"
}
`
		path := writeTF(t, dir, content)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		restores, err := NewConverter(&fakeRegistry{}).ConvertToLocal(ctx, dir, info)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if len(restores) != 0 {
			t.Fatalf("expected no restores, got %v", restores)
		}
		if got, _ := os.ReadFile(path); string(got) != content {
			t.Fatalf("file changed on cancellation")
		}
	})

	t.Run("unparseable hcl", func(t *testing.T) {
		dir := t.TempDir()
		writeTF(t, dir, `module "x" {`)
		if _, err := NewConverter(&fakeRegistry{}).ConvertToLocal(context.Background(), dir, info); err == nil {
			t.Fatal("expected parse error")
		}
	})
}

func TestRevertToRegistry(t *testing.T) {
	original := `module "test" {
  source  = "codectl/mymodule/azure"
  version = "~> 1.0"
}
`
	tests := []struct {
		name     string
		registry *fakeRegistry
		want     string
	}{
		{
			name:     "version pinned to latest",
			registry: &fakeRegistry{version: "1.5.0"},
			want:     strings.Replace(original, `"~> 1.0"`, `"~> 1.5.0"`, 1),
		},
		{
			name:     "registry failure restores original",
			registry: &fakeRegistry{err: errors.New("boom")},
			want:     original,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTF(t, t.TempDir(), "local override")
			restores := []Restore{{Path: path, OriginalContent: original, ModuleName: info.Name, Provider: info.Provider, Namespace: info.Namespace}}

			if err := NewConverter(tt.registry).RevertToRegistry(context.Background(), restores); err != nil {
				t.Fatalf("RevertToRegistry() error = %v", err)
			}
			if got, _ := os.ReadFile(path); string(got) != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := NewConverter(&fakeRegistry{version: "1.0.0"}).RevertToRegistry(ctx, []Restore{{Path: "unused"}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	})
}

func TestUpdateVersionInContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		latest  string
		want    string
	}{
		{
			name:    "spaced assignment",
			content: `version = "~> 1.0"`,
			latest:  "2.0.0",
			want:    `version = "~> 2.0.0"`,
		},
		{
			name:    "compact assignment",
			content: `version="1.0.0"`,
			latest:  "3.0.0",
			want:    `version="~> 3.0.0"`,
		},
		{
			name:    "no version attribute unchanged",
			content: `source = "../../"`,
			latest:  "2.0.0",
			want:    `source = "../../"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := updateVersionInContent(tt.content, tt.latest); got != tt.want {
				t.Fatalf("updateVersionInContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpdateModuleBlock(t *testing.T) {
	moduleSource := "codectl/mymodule/azure"
	submoduleRegex := regexp.MustCompile(`^codectl/mymodule/azure//modules/(.*)$`)

	tests := []struct {
		name        string
		source      string
		wantSource  string
		wantChanged bool
	}{
		{"root module", "codectl/mymodule/azure", "../../", true},
		{"submodule", "codectl/mymodule/azure//modules/network", "../../modules/network", true},
		{"submodule with leading slash", "codectl/mymodule/azure//modules//network", "../../modules/network", true},
		{"foreign module", "hashicorp/consul/aws", "hashicorp/consul/aws", false},
		{"local path", "../../", "../../", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := hclwrite.NewEmptyFile().Body().AppendNewBlock("module", []string{"test"})
			block.Body().SetAttributeValue("source", cty.StringVal(tt.source))
			block.Body().SetAttributeValue("version", cty.StringVal("~> 1.0"))

			if changed := updateModuleBlock(block, moduleSource, submoduleRegex); changed != tt.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, tt.wantChanged)
			}
			got, _ := attributeStringValue(block.Body().GetAttribute("source"))
			if got != tt.wantSource {
				t.Fatalf("source = %q, want %q", got, tt.wantSource)
			}
			if hasVersion := block.Body().GetAttribute("version") != nil; hasVersion == tt.wantChanged {
				t.Fatalf("version attribute present = %v after change = %v", hasVersion, tt.wantChanged)
			}
		})
	}

	t.Run("block without source", func(t *testing.T) {
		block := hclwrite.NewEmptyFile().Body().AppendNewBlock("module", []string{"test"})
		if updateModuleBlock(block, moduleSource, submoduleRegex) {
			t.Fatal("expected no change without source attribute")
		}
	})
}

func TestAttributeStringValue(t *testing.T) {
	tests := []struct {
		name   string
		value  cty.Value
		want   string
		wantOK bool
	}{
		{"plain string", cty.StringVal("test-value"), "test-value", true},
		{"path string", cty.StringVal("../../modules/test"), "../../modules/test", true},
		{"number is not a string", cty.NumberIntVal(1), "", false},
		{"list is not a string", cty.ListVal([]cty.Value{cty.StringVal("a")}), "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := hclwrite.NewEmptyFile().Body()
			body.SetAttributeValue("test", tt.value)

			got, ok := attributeStringValue(body.GetAttribute("test"))
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("attributeStringValue() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
