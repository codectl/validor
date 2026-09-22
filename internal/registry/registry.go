// Package registry fetches module version metadata from the Terraform
// Registry.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Response is the versions document returned by the Terraform Registry.
type Response struct {
	Versions []struct {
		Version string `json:"version"`
	} `json:"versions"`
}

// Client queries the public Terraform Registry.
type Client struct {
	baseURL string
	client  *http.Client
}

// New returns a Client for registry.terraform.io with a 10 second timeout.
func New() *Client {
	return &Client{
		baseURL: "https://registry.terraform.io/v1/modules",
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// GetLatestVersion returns the first version the registry lists for the
// module namespace/name/provider.
func (c *Client) GetLatestVersion(ctx context.Context, namespace, name, provider string) (string, error) {
	url := fmt.Sprintf("%s/%s/%s/%s/versions", c.baseURL, namespace, name, provider)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch module versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch module versions: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var registryResp Response
	if err := json.Unmarshal(body, &registryResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(registryResp.Versions) == 0 {
		return "", fmt.Errorf("no versions found for module %s/%s/%s", namespace, name, provider)
	}

	return registryResp.Versions[0].Version, nil
}
