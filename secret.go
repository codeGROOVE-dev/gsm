// Package gsm provides access to Google Cloud Secret Manager via REST API.
package gsm //nolint:revive // package name intentionally shorter than directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	metadataURL = "http://metadata.google.internal/computeMetadata/v1" //nolint:revive // metadata server only accessible via HTTP
	apiURL      = "https://secretmanager.googleapis.com/v1"
	retryDelay  = 1 * time.Second
	httpClient  = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			MaxIdleConnsPerHost: 2,
		},
	}
)

const (
	maxRetries  = 3
	maxBodySize = 10 * 1024 * 1024 // 10MB limit for response bodies
)

var (
	projectIDRegex  = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	secretNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,255}$`)
)

// Secret retrieves the latest version of a secret from the current project.
// The project ID is auto-detected from the GCP metadata server.
func Secret(ctx context.Context, name string) (string, error) {
	if !secretNameRegex.MatchString(name) {
		return "", errors.New("invalid secret name format")
	}

	var pid string
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			slog.Info("retrying project ID fetch", "attempt", attempt+1)
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL+"/project/", http.NoBody)
		if err != nil {
			return "", err
		}
		req.Header.Set("Metadata-Flavor", "Google")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			slog.Warn("failed to get project ID", "attempt", attempt+1)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close() //nolint:errcheck,gosec // best effort close
			lastErr = fmt.Errorf("metadata server status %d", resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
		resp.Body.Close() //nolint:errcheck,gosec // best effort close
		if err != nil {
			lastErr = err
			continue
		}

		pid = strings.TrimSpace(string(body))
		if pid != "" {
			break
		}
		lastErr = errors.New("empty project ID")
	}

	if pid == "" {
		return "", fmt.Errorf("failed to get project ID: %w", lastErr)
	}

	return SecretInProject(ctx, pid, name)
}

// SecretInProject retrieves the latest version of a secret from a specific project.
func SecretInProject(ctx context.Context, pid, name string) (string, error) {
	if !projectIDRegex.MatchString(pid) {
		return "", errors.New("invalid project ID format")
	}
	if !secretNameRegex.MatchString(name) {
		return "", errors.New("invalid secret name format")
	}

	var tok string
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			slog.Info("retrying access token fetch", "attempt", attempt+1)
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL+"/instance/service-accounts/default/token", http.NoBody)
		if err != nil {
			return "", err
		}
		req.Header.Set("Metadata-Flavor", "Google")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			slog.Warn("failed to get access token", "attempt", attempt+1)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close() //nolint:errcheck,gosec // best effort close
			lastErr = fmt.Errorf("metadata server status %d", resp.StatusCode)
			continue
		}

		var result struct {
			AccessToken string `json:"access_token"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, maxBodySize)).Decode(&result)
		resp.Body.Close() //nolint:errcheck,gosec // best effort close
		if err != nil {
			lastErr = err
			continue
		}

		if result.AccessToken != "" {
			tok = result.AccessToken
			break
		}
		lastErr = errors.New("empty access token")
	}

	if tok == "" {
		return "", fmt.Errorf("failed to get access token: %w", lastErr)
	}

	url := fmt.Sprintf("%s/projects/%s/secrets/%s/versions/latest:access", apiURL, pid, name)

	for attempt := range maxRetries {
		if attempt > 0 {
			slog.Info("retrying secret access", "attempt", attempt+1)
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+tok)

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			slog.Warn("failed to access secret", "attempt", attempt+1)
			continue
		}

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			resp.Body.Close() //nolint:errcheck,gosec // best effort close
			slog.Error("secret access denied", "status", resp.StatusCode)
			return "", fmt.Errorf("failed to access secret: status %d", resp.StatusCode)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close() //nolint:errcheck,gosec // best effort close
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			slog.Warn("secret access failed", "attempt", attempt+1, "status", resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
		resp.Body.Close() //nolint:errcheck,gosec // best effort close
		if err != nil {
			lastErr = err
			continue
		}

		var result struct {
			Payload struct {
				Data string `json:"data"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			lastErr = err
			continue
		}

		slog.Info("secret accessed successfully")
		return result.Payload.Data, nil
	}

	return "", fmt.Errorf("failed to access secret: %w", lastErr)
}
