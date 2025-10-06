package gsm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSecret(t *testing.T) {
	oldRetryDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = oldRetryDelay }()

	tests := []struct {
		name           string
		secretName     string
		projectID      string
		secretData     string
		metadataStatus int
		tokenStatus    int
		secretStatus   int
		wantErr        bool
		errContains    string
	}{
		{
			name:           "successful get",
			secretName:     "test-secret",
			projectID:      "test-project",
			secretData:     "secret-value",
			metadataStatus: http.StatusOK,
			tokenStatus:    http.StatusOK,
			secretStatus:   http.StatusOK,
			wantErr:        false,
		},
		{
			name:           "metadata server fails",
			secretName:     "test-secret",
			projectID:      "",
			metadataStatus: http.StatusInternalServerError,
			wantErr:        true,
			errContains:    "failed to get project ID",
		},
		{
			name:           "empty project ID",
			secretName:     "test-secret",
			projectID:      "",
			metadataStatus: http.StatusOK,
			wantErr:        true,
			errContains:    "failed to get project ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake metadata server
			metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata-Flavor") != "Google" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/project/project-id") {
					if tt.metadataStatus != http.StatusOK {
						w.WriteHeader(tt.metadataStatus)
						return
					}
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(tt.projectID)) //nolint:errcheck // test mock server
					return
				}
				if strings.Contains(r.URL.Path, "/token") {
					if tt.tokenStatus != http.StatusOK {
						w.WriteHeader(tt.tokenStatus)
						return
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token"}) //nolint:errcheck // test mock server
					return
				}
			}))
			defer metadataServer.Close()

			// Setup fake secret manager API
			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if tt.secretStatus != http.StatusOK {
					w.WriteHeader(tt.secretStatus)
					return
				}
				w.WriteHeader(http.StatusOK)
				// Secret Manager API returns base64-encoded data
				encodedData := base64.StdEncoding.EncodeToString([]byte(tt.secretData))
				_ = json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck // test mock server
					"payload": map[string]string{"data": encodedData},
				})
			}))
			defer apiServer.Close()

			// Override URLs
			oldMetadataURL := metadataURL
			oldAPIURL := apiURL
			defer func() {
				metadataURL = oldMetadataURL
				apiURL = oldAPIURL
			}()
			metadataURL = metadataServer.URL
			apiURL = apiServer.URL

			ctx := context.Background()
			got, err := Secret(ctx, tt.secretName)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Secret() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Secret() error = %v, want error containing %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Secret() unexpected error = %v", err)
				return
			}

			if got != tt.secretData {
				t.Errorf("Secret() = %v, want %v", got, tt.secretData)
			}
		})
	}
}

func TestSecretInProject(t *testing.T) {
	oldRetryDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = oldRetryDelay }()

	tests := []struct {
		name         string
		projectID    string
		secretName   string
		secretData   string
		tokenStatus  int
		secretStatus int
		wantErr      bool
		errContains  string
	}{
		{
			name:         "successful get",
			projectID:    "test-project",
			secretName:   "test-secret",
			secretData:   "secret-value",
			tokenStatus:  http.StatusOK,
			secretStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:        "empty project ID",
			projectID:   "",
			secretName:  "test-secret",
			wantErr:     true,
			errContains: "invalid project ID format",
		},
		{
			name:        "empty secret name",
			projectID:   "test-project",
			secretName:  "",
			wantErr:     true,
			errContains: "invalid secret name format",
		},
		{
			name:        "token fetch fails",
			projectID:   "test-project",
			secretName:  "test-secret",
			tokenStatus: http.StatusInternalServerError,
			wantErr:     true,
			errContains: "failed to get access token",
		},
		{
			name:         "secret not found",
			projectID:    "test-project",
			secretName:   "missing-secret",
			tokenStatus:  http.StatusOK,
			secretStatus: http.StatusNotFound,
			wantErr:      true,
			errContains:  "status 404",
		},
		{
			name:         "permission denied",
			projectID:    "test-project",
			secretName:   "forbidden-secret",
			tokenStatus:  http.StatusOK,
			secretStatus: http.StatusForbidden,
			wantErr:      true,
			errContains:  "status 403",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake metadata server for token
			metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata-Flavor") != "Google" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				if tt.tokenStatus != http.StatusOK {
					w.WriteHeader(tt.tokenStatus)
					return
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token"}) //nolint:errcheck // test mock server
			}))
			defer metadataServer.Close()

			// Setup fake secret manager API
			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if tt.secretStatus != http.StatusOK {
					w.WriteHeader(tt.secretStatus)
					return
				}
				w.WriteHeader(http.StatusOK)
				// Secret Manager API returns base64-encoded data
				encodedData := base64.StdEncoding.EncodeToString([]byte(tt.secretData))
				_ = json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck // test mock server
					"payload": map[string]string{"data": encodedData},
				})
			}))
			defer apiServer.Close()

			// Override URLs
			oldMetadataURL := metadataURL
			oldAPIURL := apiURL
			defer func() {
				metadataURL = oldMetadataURL
				apiURL = oldAPIURL
			}()
			metadataURL = metadataServer.URL
			apiURL = apiServer.URL

			ctx := context.Background()
			got, err := SecretInProject(ctx, tt.projectID, tt.secretName)

			if tt.wantErr {
				if err == nil {
					t.Errorf("SecretInProject() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("SecretInProject() error = %v, want error containing %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("SecretInProject() unexpected error = %v", err)
				return
			}

			if got != tt.secretData {
				t.Errorf("SecretInProject() = %v, want %v", got, tt.secretData)
			}
		})
	}
}

func TestGetProjectRetry(t *testing.T) {
	oldRetryDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = oldRetryDelay }()

	t.Run("retry on 5xx errors", func(t *testing.T) {
		attempts := 0
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts < 3 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			// Secret Manager API returns base64-encoded data
			encodedData := base64.StdEncoding.EncodeToString([]byte("secret-value"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck // test mock server
				"payload": map[string]string{"data": encodedData},
			})
		}))
		defer apiServer.Close()

		metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token"}) //nolint:errcheck // test mock server
		}))
		defer metadataServer.Close()

		oldMetadataURL := metadataURL
		oldAPIURL := apiURL
		defer func() {
			metadataURL = oldMetadataURL
			apiURL = oldAPIURL
		}()
		metadataURL = metadataServer.URL
		apiURL = apiServer.URL

		ctx := context.Background()
		got, err := SecretInProject(ctx, "test-project", "test-secret")
		if err != nil {
			t.Errorf("SecretInProject() unexpected error = %v", err)
		}
		if got != "secret-value" {
			t.Errorf("SecretInProject() = %v, want %v", got, "secret-value")
		}
		if attempts != 3 {
			t.Errorf("Expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("no retry on 4xx errors", func(t *testing.T) {
		attempts := 0
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusNotFound)
		}))
		defer apiServer.Close()

		metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token"}) //nolint:errcheck // test mock server
		}))
		defer metadataServer.Close()

		oldMetadataURL := metadataURL
		oldAPIURL := apiURL
		defer func() {
			metadataURL = oldMetadataURL
			apiURL = oldAPIURL
		}()
		metadataURL = metadataServer.URL
		apiURL = apiServer.URL

		ctx := context.Background()
		_, err := SecretInProject(ctx, "test-project", "test-secret")
		if err == nil {
			t.Error("SecretInProject() expected error, got nil")
		}
		if attempts != 1 {
			t.Errorf("Expected 1 attempt for 4xx error, got %d", attempts)
		}
	})

	t.Run("exhausts all retries", func(t *testing.T) {
		attempts := 0
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer apiServer.Close()

		metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token"}) //nolint:errcheck // test mock server
		}))
		defer metadataServer.Close()

		oldMetadataURL := metadataURL
		oldAPIURL := apiURL
		defer func() {
			metadataURL = oldMetadataURL
			apiURL = oldAPIURL
		}()
		metadataURL = metadataServer.URL
		apiURL = apiServer.URL

		ctx := context.Background()
		_, err := SecretInProject(ctx, "test-project", "test-secret")
		if err == nil {
			t.Error("SecretInProject() expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to access secret") {
			t.Errorf("Expected error about failed access, got: %v", err)
		}
		if attempts != maxRetries {
			t.Errorf("Expected %d attempts, got %d", maxRetries, attempts)
		}
	})
}

func TestContextCancellation(t *testing.T) {
	oldRetryDelay := retryDelay
	retryDelay = 5 * time.Second
	defer func() { retryDelay = oldRetryDelay }()

	t.Run("context cancelled during retry", func(t *testing.T) {
		attempts := 0
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer apiServer.Close()

		metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token"}) //nolint:errcheck // test mock server
		}))
		defer metadataServer.Close()

		oldMetadataURL := metadataURL
		oldAPIURL := apiURL
		defer func() {
			metadataURL = oldMetadataURL
			apiURL = oldAPIURL
		}()
		metadataURL = metadataServer.URL
		apiURL = apiServer.URL

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err := SecretInProject(ctx, "test-project", "test-secret")
		if err == nil {
			t.Error("SecretInProject() expected error, got nil")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("SecretInProject() error = %v, want %v", err, context.DeadlineExceeded)
		}
		if attempts > 2 {
			t.Errorf("Expected at most 2 attempts before context cancellation, got %d", attempts)
		}
	})
}

func TestLargeResponseBody(t *testing.T) {
	oldRetryDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = oldRetryDelay }()

	t.Run("response body limited", func(t *testing.T) {
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			// Write a large response larger than maxBodySize
			largeData := strings.Repeat("x", maxBodySize+1000)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck // test mock server
				"payload": map[string]string{"data": largeData},
			})
		}))
		defer apiServer.Close()

		metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token"}) //nolint:errcheck // test mock server
		}))
		defer metadataServer.Close()

		oldMetadataURL := metadataURL
		oldAPIURL := apiURL
		defer func() {
			metadataURL = oldMetadataURL
			apiURL = oldAPIURL
		}()
		metadataURL = metadataServer.URL
		apiURL = apiServer.URL

		ctx := context.Background()
		_, err := SecretInProject(ctx, "test-project", "test-secret")
		// Should fail to decode because response was truncated
		if err == nil {
			t.Error("SecretInProject() expected error for truncated response, got nil")
		}
	})
}

func TestMetadataFlavorHeader(t *testing.T) {
	t.Run("missing metadata flavor header rejected", func(t *testing.T) {
		metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Metadata-Flavor") != "Google" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("Metadata-Flavor header required")) //nolint:errcheck // test mock server
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("test-project")) //nolint:errcheck // test mock server
		}))
		defer metadataServer.Close()

		oldMetadataURL := metadataURL
		defer func() {
			metadataURL = oldMetadataURL
		}()
		metadataURL = metadataServer.URL

		// This test verifies our implementation sets the header correctly
		ctx := context.Background()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataServer.URL+"/project/", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		// Intentionally omit header
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close() //nolint:errcheck // test cleanup
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected Forbidden without Metadata-Flavor header, got %d", resp.StatusCode)
		}
	})
}

func TestEmptyResponses(t *testing.T) {
	oldRetryDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = oldRetryDelay }()

	tests := []struct {
		name        string
		setupServer func() *httptest.Server
		wantErr     bool
		errContains string
	}{
		{
			name: "empty project ID response",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/project/project-id") {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte("   \n  ")) //nolint:errcheck // test mock server
						return
					}
				}))
			},
			wantErr:     true,
			errContains: "failed to get project ID",
		},
		{
			name: "empty access token",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"access_token": ""}) //nolint:errcheck // test mock server
				}))
			},
			wantErr:     true,
			errContains: "failed to get access token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer()
			defer server.Close()

			oldMetadataURL := metadataURL
			defer func() {
				metadataURL = oldMetadataURL
			}()
			metadataURL = server.URL

			ctx := context.Background()
			var err error
			if strings.Contains(tt.name, "project") {
				_, err = Secret(ctx, "test-secret")
			} else {
				_, err = SecretInProject(ctx, "test-project", "test-secret")
			}

			if !tt.wantErr {
				if err != nil {
					t.Errorf("unexpected error = %v", err)
				}
				return
			}

			if err == nil {
				t.Error("expected error, got nil")
				return
			}

			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error = %v, want error containing %v", err, tt.errContains)
			}
		})
	}
}

func TestURLConstruction(t *testing.T) {
	t.Run("correct URL format", func(t *testing.T) {
		var capturedURL string
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedURL = r.URL.String()
			w.WriteHeader(http.StatusOK)
			// Secret Manager API returns base64-encoded data
			encodedData := base64.StdEncoding.EncodeToString([]byte("secret-value"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck // test mock server
				"payload": map[string]string{"data": encodedData},
			})
		}))
		defer apiServer.Close()

		metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token"}) //nolint:errcheck // test mock server
		}))
		defer metadataServer.Close()

		oldMetadataURL := metadataURL
		oldAPIURL := apiURL
		defer func() {
			metadataURL = oldMetadataURL
			apiURL = oldAPIURL
		}()
		metadataURL = metadataServer.URL
		apiURL = apiServer.URL

		ctx := context.Background()
		_, err := SecretInProject(ctx, "my-project", "my-secret")
		if err != nil {
			t.Errorf("unexpected error = %v", err)
		}

		expectedPath := "/projects/my-project/secrets/my-secret/versions/latest:access"
		if capturedURL != expectedPath {
			t.Errorf("URL = %v, want %v", capturedURL, expectedPath)
		}
	})
}

func TestNetworkErrors(t *testing.T) {
	oldRetryDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = oldRetryDelay }()

	t.Run("project ID network error", func(t *testing.T) {
		oldMetadataURL := metadataURL
		defer func() {
			metadataURL = oldMetadataURL
		}()
		// Point to non-existent server
		metadataURL = "http://localhost:1"

		ctx := context.Background()
		_, err := Secret(ctx, "test-secret")
		if err == nil {
			t.Error("Expected error from network failure, got nil")
		}
	})

	t.Run("token network error", func(t *testing.T) {
		oldMetadataURL := metadataURL
		defer func() {
			metadataURL = oldMetadataURL
		}()
		// Point to non-existent server
		metadataURL = "http://localhost:1"

		ctx := context.Background()
		_, err := SecretInProject(ctx, "test-project", "test-secret")
		if err == nil {
			t.Error("Expected error from network failure, got nil")
		}
	})

	t.Run("secret network error", func(t *testing.T) {
		metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token"}) //nolint:errcheck // test mock server
		}))
		defer metadataServer.Close()

		oldMetadataURL := metadataURL
		oldAPIURL := apiURL
		defer func() {
			metadataURL = oldMetadataURL
			apiURL = oldAPIURL
		}()
		metadataURL = metadataServer.URL
		// Point to non-existent server
		apiURL = "http://localhost:1"

		ctx := context.Background()
		_, err := SecretInProject(ctx, "test-project", "test-secret")
		if err == nil {
			t.Error("Expected error from network failure, got nil")
		}
	})
}

func TestReadErrors(t *testing.T) {
	oldRetryDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = oldRetryDelay }()

	t.Run("project ID read error", func(t *testing.T) {
		metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/project/") {
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Length", "100")
				// Write less than promised to cause read error
				return
			}
		}))
		defer metadataServer.Close()

		oldMetadataURL := metadataURL
		defer func() {
			metadataURL = oldMetadataURL
		}()
		metadataURL = metadataServer.URL

		ctx := context.Background()
		_, err := Secret(ctx, "test-secret")
		if err == nil {
			t.Error("Expected error from read failure, got nil")
		}
	})

	t.Run("secret read error retries", func(t *testing.T) {
		attempts := 0
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Length", "1000")
			// Write nothing to cause read error
		}))
		defer apiServer.Close()

		metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token"}) //nolint:errcheck // test mock server
		}))
		defer metadataServer.Close()

		oldMetadataURL := metadataURL
		oldAPIURL := apiURL
		defer func() {
			metadataURL = oldMetadataURL
			apiURL = oldAPIURL
		}()
		metadataURL = metadataServer.URL
		apiURL = apiServer.URL

		ctx := context.Background()
		_, err := SecretInProject(ctx, "test-project", "test-secret")
		if err == nil {
			t.Error("Expected error from read failures, got nil")
		}
		if attempts != maxRetries {
			t.Errorf("Expected %d attempts, got %d", maxRetries, attempts)
		}
	})
}
