// Package main demonstrates using the gcp-secret library.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github.com/codeGROOVE-dev/gsm"
)

func main() {
	ctx := context.Background()

	// Store a secret (auto-detect project)
	if err := gsm.Store(ctx, "my-secret", "my-secret-value"); err != nil {
		log.Fatalf("failed to store secret: %v", err)
	}
	slog.Info("secret stored successfully")

	// Fetch secret from current project (auto-detected)
	value, err := gsm.Fetch(ctx, "my-secret")
	if err != nil {
		log.Fatalf("failed to fetch secret: %v", err)
	}

	slog.Info("secret retrieved", "length", len(value))

	// Or specify a different project explicitly
	if otherProject := os.Getenv("OTHER_PROJECT_ID"); otherProject != "" {
		// Store in specific project
		if err := gsm.StoreInProject(ctx, otherProject, "my-secret", "other-value"); err != nil {
			log.Fatalf("failed to store secret in other project: %v", err)
		}
		slog.Info("secret stored in other project")

		// Fetch from specific project
		value, err = gsm.FetchFromProject(ctx, otherProject, "my-secret")
		if err != nil {
			log.Fatalf("failed to fetch secret: %v", err)
		}
		slog.Info("secret from other project retrieved", "length", len(value))
	}
}
