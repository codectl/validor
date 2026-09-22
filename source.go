package validor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/codectl/validor/internal/registry"
	"github.com/codectl/validor/internal/source"
)

// ModuleInfo identifies a module in the Terraform Registry.
type ModuleInfo = source.Info

// FileRestore records the original content of a file rewritten for a local
// run.
type FileRestore = source.Restore

// TerraformRegistryResponse is the versions document returned by the
// Terraform Registry.
type TerraformRegistryResponse = registry.Response

// DefaultSourceConverter rewrites module sources with hclwrite.
type DefaultSourceConverter = source.Converter

// DefaultRegistryClient queries registry.terraform.io.
type DefaultRegistryClient = registry.Client

// NewSourceConverter returns the SourceConverter backed by client.
func NewSourceConverter(client RegistryClient) SourceConverter {
	return source.NewConverter(client)
}

// NewRegistryClient returns the RegistryClient for registry.terraform.io.
func NewRegistryClient() RegistryClient {
	return registry.New()
}

func convertModulesToLocal(ctx context.Context, t *testing.T, converter SourceConverter, moduleNames []string, exceptionList []string, moduleInfo ModuleInfo, examplesPath string) []FileRestore {
	var allFilesToRestore []FileRestore

	for _, moduleName := range moduleNames {
		if slices.Contains(exceptionList, moduleName) {
			continue
		}

		modulePath := filepath.Join(examplesPath, moduleName)
		filesToRestore, err := converter.ConvertToLocal(ctx, modulePath, moduleInfo)
		if err != nil {
			t.Logf("Warning: Failed to convert module %s to local source: %v", moduleName, err)
			continue
		}
		allFilesToRestore = append(allFilesToRestore, filesToRestore...)
	}

	return allFilesToRestore
}

func createLocalSetupFunc(config *Config) TestSetupFunc {
	return func(ctx context.Context, t *testing.T, modules []*Module) error {
		moduleInfo := extractModuleInfoFromRepo()
		if moduleInfo.Name == "" || moduleInfo.Provider == "" {
			return fmt.Errorf("could not determine module name and provider from repository")
		}
		moduleInfo.Namespace = config.Namespace

		converter := NewSourceConverter(newRegistryClient())
		moduleNames := extractModuleNames(modules)
		allFilesToRestore := convertModulesToLocal(ctx, t, converter, moduleNames, config.ExceptionList, moduleInfo, getExamplesPath(config))

		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := converter.RevertToRegistry(cleanupCtx, allFilesToRestore); err != nil {
				t.Logf("Warning: Failed to revert files to registry source: %v", err)
			}
		})
		return nil
	}
}

// newRegistryClient is swapped in tests to keep local runs off the network.
var newRegistryClient = NewRegistryClient

func extractModuleInfoFromRepo() ModuleInfo {
	wd, err := os.Getwd()
	if err != nil {
		return ModuleInfo{}
	}
	return source.Detect(wd)
}
