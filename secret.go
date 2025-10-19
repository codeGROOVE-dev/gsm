// Package gsm provides access to Google Cloud Secret Manager via REST API.
package gsm

import (
	"bytes"
	"context"
	"encoding/base64"
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

// Note: This package intentionally uses simple retry logic without importing
// external dependencies (including github.com/codeGROOVE-dev/retry) to maintain
// zero dependencies. The metadata server and Secret Manager API are reliable
// services that don't require exponential backoff with jitter.

var (
	projectIDRegex  = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	secretNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,255}$`)
)

// Fetch retrieves the latest version of a secret from the current project.
// The project ID is auto-detected from the GCP metadata server.
func Fetch(ctx context.Context, name string) (string, error) {
	if !secretNameRegex.MatchString(name) {
		return "", errors.New("invalid secret name format")
	}

	p, err := getProjectID(ctx)
	if err != nil {
		return "", err
	}

	return FetchFromProject(ctx, p, name)
}

// getProjectID fetches the project ID from the GCP metadata server.
func getProjectID(ctx context.Context) (string, error) {
	var p string
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

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL+"/project/project-id", http.NoBody)
		if err != nil {
			return "", err
		}
		req.Header.Set("Metadata-Flavor", "Google")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			slog.Warn("failed to get project ID", "attempt", attempt+1, "error", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close() //nolint:errcheck,gosec // best effort close
			lastErr = fmt.Errorf("metadata server status %d", resp.StatusCode)
			slog.Warn("failed to get project ID", "attempt", attempt+1, "status", resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
		resp.Body.Close() //nolint:errcheck,gosec // best effort close
		if err != nil {
			lastErr = err
			continue
		}

		p = strings.TrimSpace(string(body))
		if p != "" {
			slog.Info("fetched project ID from metadata server", "project_id", p, "length", len(p))
			break
		}
		lastErr = errors.New("empty project ID")
	}

	if p == "" {
		return "", fmt.Errorf("failed to get project ID: %w", lastErr)
	}

	return p, nil
}

// getAccessToken fetches an access token from the GCP metadata server.
func getAccessToken(ctx context.Context) (string, error) {
	var t string
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
			slog.Warn("failed to get access token", "attempt", attempt+1, "error", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close() //nolint:errcheck,gosec // best effort close
			lastErr = fmt.Errorf("metadata server status %d", resp.StatusCode)
			slog.Warn("failed to get access token", "attempt", attempt+1, "status", resp.StatusCode)
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
			t = result.AccessToken
			break
		}
		lastErr = errors.New("empty access token")
	}

	if t == "" {
		return "", fmt.Errorf("failed to get access token: %w", lastErr)
	}

	return t, nil
}

// FetchFromProject retrieves the latest version of a secret from a specific project.
func FetchFromProject(ctx context.Context, pid, name string) (string, error) {
	if !projectIDRegex.MatchString(pid) {
		return "", fmt.Errorf("invalid project ID format: %q", pid)
	}
	if !secretNameRegex.MatchString(name) {
		return "", errors.New("invalid secret name format")
	}

	t, err := getAccessToken(ctx)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/projects/%s/secrets/%s/versions/latest:access", apiURL, pid, name)

	var lastErr error
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
		req.Header.Set("Authorization", "Bearer "+t)

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			slog.Warn("failed to access secret", "attempt", attempt+1, "error", err)
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

		// The Secret Manager API returns base64-encoded data
		decoded, err := base64.StdEncoding.DecodeString(result.Payload.Data)
		if err != nil {
			lastErr = fmt.Errorf("failed to decode secret data: %w", err)
			continue
		}

		slog.Info("secret accessed successfully")
		return string(decoded), nil
	}

	return "", fmt.Errorf("failed to access secret: %w", lastErr)
}

// Store creates or updates a secret in the current project.
// The project ID is auto-detected from the GCP metadata server.
// If the secret doesn't exist, it will be created. If it exists, a new version will be added.
func Store(ctx context.Context, name, value string) error {
	if !secretNameRegex.MatchString(name) {
		return errors.New("invalid secret name format")
	}

	p, err := getProjectID(ctx)
	if err != nil {
		return err
	}

	return StoreInProject(ctx, p, name, value)
}

// StoreInProject creates or updates a secret in a specific project.
// If the secret doesn't exist, it will be created. If it exists, a new version will be added.
func StoreInProject(ctx context.Context, pid, name, value string) error {
	if !projectIDRegex.MatchString(pid) {
		return fmt.Errorf("invalid project ID format: %q", pid)
	}
	if !secretNameRegex.MatchString(name) {
		return errors.New("invalid secret name format")
	}

	tok, err := getAccessToken(ctx)
	if err != nil {
		return err
	}

	// First, try to create the secret
	createURL := fmt.Sprintf("%s/projects/%s/secrets?secretId=%s", apiURL, pid, name)
	createReqBody := map[string]any{
		"replication": map[string]any{
			"automatic": map[string]any{},
		},
	}
	createData, err := json.Marshal(createReqBody)
	if err != nil {
		return err
	}

	var createErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			slog.Info("retrying secret creation", "attempt", attempt+1)
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(createData))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			createErr = err
			slog.Warn("failed to create secret", "attempt", attempt+1, "error", err)
			continue
		}

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			resp.Body.Close() //nolint:errcheck,gosec // best effort close
			slog.Info("secret created successfully")
			break
		}

		// Read error body for logging
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodySize)) //nolint:errcheck // best effort
		resp.Body.Close()                                             //nolint:errcheck,gosec // best effort close

		if resp.StatusCode == http.StatusConflict {
			// Secret already exists, which is fine - we'll add a version
			createErr = fmt.Errorf("secret already exists: status %d", resp.StatusCode)
			break
		}

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			slog.Error("secret creation denied", "status", resp.StatusCode, "body", string(body))
			return fmt.Errorf("failed to create secret: status %d: %s", resp.StatusCode, body)
		}

		createErr = fmt.Errorf("status %d: %s", resp.StatusCode, body)
		slog.Warn("secret creation failed", "attempt", attempt+1, "status", resp.StatusCode)
	}

	// If secret creation failed for reasons other than "already exists", return error
	if createErr != nil && !strings.Contains(createErr.Error(), "secret already exists") {
		return fmt.Errorf("failed to create secret: %w", createErr)
	}

	// Now add a new version with the value
	versionURL := fmt.Sprintf("%s/projects/%s/secrets/%s:addVersion", apiURL, pid, name)
	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	versionReqBody := map[string]any{
		"payload": map[string]string{
			"data": encoded,
		},
	}
	versionData, err := json.Marshal(versionReqBody)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			slog.Info("retrying add secret version", "attempt", attempt+1)
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, versionURL, bytes.NewReader(versionData))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			slog.Warn("failed to add secret version", "attempt", attempt+1, "error", err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			resp.Body.Close() //nolint:errcheck,gosec // best effort close
			slog.Info("secret version added successfully")
			return nil
		}

		// Read error body for logging
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodySize)) //nolint:errcheck // best effort
		resp.Body.Close()                                             //nolint:errcheck,gosec // best effort close

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			slog.Error("add secret version denied", "status", resp.StatusCode, "body", string(body))
			return fmt.Errorf("failed to add secret version: status %d: %s", resp.StatusCode, body)
		}

		lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, body)
		slog.Warn("add secret version failed", "attempt", attempt+1, "status", resp.StatusCode)
	}

	return fmt.Errorf("failed to add secret version: %w", lastErr)
}
