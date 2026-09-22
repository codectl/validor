package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetLatestVersion(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{
			name:   "first listed version wins",
			status: http.StatusOK,
			body:   `{"versions":[{"version":"2.0.0"},{"version":"1.0.0"}]}`,
			want:   "2.0.0",
		},
		{
			name:    "non-200 response",
			status:  http.StatusInternalServerError,
			body:    "",
			wantErr: true,
		},
		{
			name:    "empty versions",
			status:  http.StatusOK,
			body:    `{"versions":[]}`,
			wantErr: true,
		},
		{
			name:    "malformed json",
			status:  http.StatusOK,
			body:    `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := New()
			c.baseURL = srv.URL

			got, err := c.GetLatestVersion(context.Background(), "ns", "name", "provider")
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetLatestVersion() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("GetLatestVersion() = %q, want %q", got, tt.want)
			}
			if gotPath != "/ns/name/provider/versions" {
				t.Fatalf("requested %q, want /ns/name/provider/versions", gotPath)
			}
		})
	}
}
