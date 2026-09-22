package source

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// Detect derives the module a repository publishes from its name, which
// must follow terraform-<provider>-<name>. dir may be the repository root
// or its tests directory. The git origin remote is consulted first, the
// directory name second. Namespace is left empty.
func Detect(dir string) Info {
	if filepath.Base(dir) == "tests" {
		dir = filepath.Dir(dir)
	}

	if repoName := repoNameFromGit(dir); repoName != "" {
		if info, ok := parseModuleName(repoName); ok {
			return info
		}
	}

	if info, ok := parseModuleName(filepath.Base(dir)); ok {
		return info
	}
	return Info{}
}

func parseModuleName(repoName string) (Info, bool) {
	const prefix = "terraform-"
	if !strings.HasPrefix(repoName, prefix) {
		return Info{}, false
	}

	parts := strings.SplitN(repoName[len(prefix):], "-", 2)
	if len(parts) != 2 {
		return Info{}, false
	}

	return Info{
		Provider: parts[0],
		Name:     parts[1],
	}, true
}

func repoNameFromGit(dir string) string {
	output, err := gitRemoteURL(dir)
	if err != nil {
		return ""
	}

	url := strings.TrimSpace(string(output))
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		repoName := parts[len(parts)-1]
		return strings.TrimSuffix(repoName, ".git")
	}
	return ""
}

var gitRemoteURL = func(dir string) ([]byte, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = dir
	return cmd.Output()
}
