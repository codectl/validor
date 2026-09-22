package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseModuleName(t *testing.T) {
	tests := []struct {
		name   string
		repo   string
		want   Info
		wantOK bool
	}{
		{"azure module", "terraform-azure-mymodule", Info{Provider: "azure", Name: "mymodule"}, true},
		{"aws module", "terraform-aws-vpc", Info{Provider: "aws", Name: "vpc"}, true},
		{"hyphenated name", "terraform-azure-storage-account", Info{Provider: "azure", Name: "storage-account"}, true},
		{"no terraform prefix", "azure-mymodule", Info{}, false},
		{"no provider", "terraform-mymodule", Info{}, false},
		{"empty", "", Info{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseModuleName(tt.repo)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("parseModuleName(%q) = (%+v, %v), want (%+v, %v)", tt.repo, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestRepoNameFromGit(t *testing.T) {
	tests := []struct {
		name   string
		output string
		err    error
		want   string
	}{
		{"ssh remote", "git@github.com:codectl/terraform-azure-mymodule.git\n", nil, "terraform-azure-mymodule"},
		{"https remote", "https://github.com/codectl/terraform-azure-mymodule.git\n", nil, "terraform-azure-mymodule"},
		{"remote without .git suffix", "https://github.com/codectl/terraform-azure-mymodule", nil, "terraform-azure-mymodule"},
		{"git failure", "", errors.New("not a git repository"), ""},
	}

	orig := gitRemoteURL
	t.Cleanup(func() { gitRemoteURL = orig })

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitRemoteURL = func(string) ([]byte, error) { return []byte(tt.output), tt.err }
			if got := repoNameFromGit("."); got != tt.want {
				t.Fatalf("repoNameFromGit() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name   string
		dir    string // relative to a temp root
		remote string // git origin output; empty means not a git repo
		want   Info
	}{
		{"from directory name", "terraform-azure-mymodule", "", Info{Provider: "azure", Name: "mymodule"}},
		{"from tests subdirectory", "terraform-azure-testmodule/tests", "", Info{Provider: "azure", Name: "testmodule"}},
		{"git remote wins over directory name", "checkout", "git@github.com:codectl/terraform-aws-vpc.git\n", Info{Provider: "aws", Name: "vpc"}},
		{"unparseable remote falls back to directory name", "terraform-azure-fallback", "git@github.com:codectl/validor.git\n", Info{Provider: "azure", Name: "fallback"}},
		{"nothing to detect", "somewhere", "", Info{}},
	}

	orig := gitRemoteURL
	t.Cleanup(func() { gitRemoteURL = orig })

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), tt.dir)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			gitRemoteURL = func(string) ([]byte, error) {
				if tt.remote == "" {
					return nil, errors.New("not a git repository")
				}
				return []byte(tt.remote), nil
			}

			if got := Detect(dir); got != tt.want {
				t.Fatalf("Detect() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
