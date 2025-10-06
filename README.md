# gcp-secret

Zero-dependency Go client for Google Cloud Secret Manager.

This library uses the Secret Manager REST API directly instead of the official SDK to avoid pulling in unnecessary dependencies. It's production-hardened with automatic retries, proper error handling, and context cancellation support.

## Installation

```bash
go get github.com/codeGROOVE-dev/gsm
```

## Usage

```go
package main

import (
    "context"
    "log"

    "github.com/codeGROOVE-dev/gsm"
)

func main() {
    ctx := context.Background()

    // Auto-detect project from metadata server
    value, err := gsm.Secret(ctx, "my-secret")
    if err != nil {
        log.Fatal(err)
    }

    // Or specify project explicitly
    value, err = gsm.SecretInProject(ctx, "my-project", "my-secret")
    if err != nil {
        log.Fatal(err)
    }
}
```

## Features

- Zero external dependencies (uses only Go standard library)
- Automatic authentication via GCP metadata server
- Retries on transient failures (3 attempts with 1s delay)
- Context cancellation support
- Response body size limiting (10MB)
- Structured logging with log/slog

## Requirements

Designed for Cloud Run and GCE environments where the metadata server is available. The service account needs the `roles/secretmanager.secretAccessor` role:

```bash
gcloud projects add-iam-policy-binding PROJECT_ID \
    --member="serviceAccount:SERVICE_ACCOUNT" \
    --role="roles/secretmanager.secretAccessor"
```

