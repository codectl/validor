// Package source rewrites the module blocks of example configurations
// between their Terraform Registry source and the local checkout, and
// detects which module a repository publishes.
package source

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

var versionRegex = regexp.MustCompile(`(version\s*=\s*")[^"]*(")`)

// Info identifies a module in the Terraform Registry.
type Info struct {
	Name      string
	Provider  string
	Namespace string
}

// Restore records the original content of a rewritten file so it can be
// put back after a local run.
type Restore struct {
	Path            string
	OriginalContent string
	ModuleName      string
	Provider        string
	Namespace       string
}

// Registry fetches the latest published version of a module.
type Registry interface {
	GetLatestVersion(ctx context.Context, namespace, name, provider string) (string, error)
}

// Converter rewrites module sources between registry and local paths.
type Converter struct {
	registry Registry
}

// NewConverter returns a Converter that pins restored sources to the
// latest version reported by registry.
func NewConverter(registry Registry) *Converter {
	return &Converter{registry: registry}
}

// ConvertToLocal points every module block under modulePath that sources
// info from the registry at the local checkout instead: the root module at
// ../../, submodules at ../../modules/<name>. The version attribute is
// dropped. Returns one Restore per rewritten file.
func (c *Converter) ConvertToLocal(ctx context.Context, modulePath string, info Info) ([]Restore, error) {
	var filesToRestore []Restore

	files, err := filepath.Glob(filepath.Join(modulePath, "*.tf"))
	if err != nil {
		return nil, fmt.Errorf("failed to find terraform files: %w", err)
	}

	moduleSource := fmt.Sprintf("%s/%s/%s", info.Namespace, info.Name, info.Provider)
	submodulePattern := fmt.Sprintf(`^%s/%s/%s//modules/(.*)$`,
		regexp.QuoteMeta(info.Namespace),
		regexp.QuoteMeta(info.Name),
		regexp.QuoteMeta(info.Provider))
	submoduleRegex, err := regexp.Compile(submodulePattern)
	if err != nil {
		return nil, fmt.Errorf("failed to compile submodule regex: %w", err)
	}

	for _, file := range files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		originalContent := string(content)
		parsedFile, diags := hclwrite.ParseConfig(content, file, hcl.InitialPos)
		if diags.HasErrors() {
			return filesToRestore, fmt.Errorf("failed to parse %s: %s", file, diags.Error())
		}

		if !updateModuleBlocks(parsedFile.Body(), moduleSource, submoduleRegex) {
			continue
		}

		if err := os.WriteFile(file, parsedFile.Bytes(), 0o644); err != nil {
			return filesToRestore, fmt.Errorf("failed to write file %s: %w", file, err)
		}

		filesToRestore = append(filesToRestore, Restore{
			Path:            file,
			OriginalContent: originalContent,
			ModuleName:      info.Name,
			Provider:        info.Provider,
			Namespace:       info.Namespace,
		})
	}

	return filesToRestore, nil
}

// RevertToRegistry writes every Restore back with its version constraint
// bumped to the latest registry release; if the registry lookup fails the
// original content is written unchanged.
func (c *Converter) RevertToRegistry(ctx context.Context, filesToRestore []Restore) error {
	for _, restore := range filesToRestore {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		latestVersion, err := c.registry.GetLatestVersion(ctx, restore.Namespace, restore.ModuleName, restore.Provider)
		if err != nil {
			if writeErr := os.WriteFile(restore.Path, []byte(restore.OriginalContent), 0o644); writeErr != nil {
				return fmt.Errorf("failed to restore file %s: %w", restore.Path, writeErr)
			}
			continue
		}

		updatedContent := updateVersionInContent(restore.OriginalContent, latestVersion)

		if err := os.WriteFile(restore.Path, []byte(updatedContent), 0o644); err != nil {
			return fmt.Errorf("failed to write updated file %s: %w", restore.Path, err)
		}
	}
	return nil
}

func updateVersionInContent(content, latestVersion string) string {
	if versionRegex.MatchString(content) {
		return versionRegex.ReplaceAllString(content, fmt.Sprintf("${1}~> %s${2}", latestVersion))
	}
	return content
}

func updateModuleBlocks(body *hclwrite.Body, moduleSource string, submoduleRegex *regexp.Regexp) bool {
	changed := false
	for _, block := range body.Blocks() {
		if block.Type() == "module" && updateModuleBlock(block, moduleSource, submoduleRegex) {
			changed = true
		}
		if updateModuleBlocks(block.Body(), moduleSource, submoduleRegex) {
			changed = true
		}
	}
	return changed
}

func updateModuleBlock(block *hclwrite.Block, moduleSource string, submoduleRegex *regexp.Regexp) bool {
	attr := block.Body().GetAttribute("source")
	if attr == nil {
		return false
	}

	sourceValue, ok := attributeStringValue(attr)
	if !ok {
		return false
	}

	switch {
	case sourceValue == moduleSource:
		block.Body().SetAttributeValue("source", cty.StringVal("../../"))
		block.Body().RemoveAttribute("version")
		return true
	case submoduleRegex != nil:
		if matches := submoduleRegex.FindStringSubmatch(sourceValue); len(matches) == 2 {
			localPath := fmt.Sprintf("../../modules/%s", strings.TrimPrefix(matches[1], "/"))
			block.Body().SetAttributeValue("source", cty.StringVal(localPath))
			block.Body().RemoveAttribute("version")
			return true
		}
	}

	return false
}

func attributeStringValue(attr *hclwrite.Attribute) (string, bool) {
	tokens := attr.Expr().BuildTokens(nil)
	if len(tokens) == 0 {
		return "", false
	}
	raw := strings.TrimSpace(string(tokens.Bytes()))
	if raw == "" {
		return "", false
	}
	value, err := strconv.Unquote(raw)
	if err != nil {
		return "", false
	}
	return value, true
}
