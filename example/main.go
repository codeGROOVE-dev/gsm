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

	// Get secret from current project (auto-detected)
	value, err := gsm.Secret(ctx, "my-secret")
	if err != nil {
		log.Fatalf("failed to get secret: %v", err)
	}

	slog.Info("secret retrieved", "length", len(value))

	// Or specify a different project explicitly
	if otherProject := os.Getenv("OTHER_PROJECT_ID"); otherProject != "" {
		value, err = gsm.SecretInProject(ctx, otherProject, "my-secret")
		if err != nil {
			log.Fatalf("failed to get secret: %v", err)
		}
		slog.Info("secret from other project retrieved", "length", len(value))
	}
}
