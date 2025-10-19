# gsm

Zero-dependency Go client for Google Cloud Secret Manager.

Why another Secret Manager client? The official Google SDK pulls in **90+ dependencies**. This library uses the Secret Manager REST API directly with **zero external dependencies** - just the Go standard library.

## Installation

```bash
go get github.com/codeGROOVE-dev/gsm
```

## Quick Start

```go
import "github.com/codeGROOVE-dev/gsm"

// Fetch a secret (auto-detects project from metadata server)
value, err := gsm.Fetch(ctx, "my-secret")

// Store a secret (creates if missing, adds version if exists)
err = gsm.Store(ctx, "my-secret", "secret-value")

// Or specify project explicitly
value, err = gsm.FetchFromProject(ctx, "my-project", "my-secret")
err = gsm.StoreInProject(ctx, "my-project", "my-secret", "secret-value")
```

## Features

- **Zero dependencies** - Uses only Go standard library (no protobuf, no gRPC, no bloat)
- **Production-ready** - Automatic retries (3 attempts, 1s delay), context cancellation, 10MB response limits
- **Auto-auth** - Authenticates via GCP metadata server (Cloud Run, GCE, GKE)
- **Idempotent writes** - `Store()` creates secrets if missing, adds versions if they exist
- **Structured logging** - Uses `log/slog` for observability

## Permissions

### Reading Secrets

Grant `roles/secretmanager.secretAccessor`:

```bash
gcloud projects add-iam-policy-binding PROJECT_ID \
    --member="serviceAccount:SERVICE_ACCOUNT" \
    --role="roles/secretmanager.secretAccessor"
```

### Writing Secrets

For principle of least privilege, grant these specific permissions:

1. **Create a custom role** for secret creation:

```bash
gcloud iam roles create secretCreator --project=PROJECT_ID \
    --title="Secret Creator" \
    --description="Can create new secrets" \
    --permissions=secretmanager.secrets.create

gcloud projects add-iam-policy-binding PROJECT_ID \
    --member="serviceAccount:SERVICE_ACCOUNT" \
    --role="projects/PROJECT_ID/roles/secretCreator"
```

2. **Grant version management** (add new secret versions):

```bash
gcloud projects add-iam-policy-binding PROJECT_ID \
    --member="serviceAccount:SERVICE_ACCOUNT" \
    --role="roles/secretmanager.secretVersionAdder"
```

3. **Grant read access** (required to check if secret exists):

```bash
gcloud projects add-iam-policy-binding PROJECT_ID \
    --member="serviceAccount:SERVICE_ACCOUNT" \
    --role="roles/secretmanager.secretAccessor"
```

Alternatively, for full control (not recommended for production):

```bash
gcloud projects add-iam-policy-binding PROJECT_ID \
    --member="serviceAccount:SERVICE_ACCOUNT" \
    --role="roles/secretmanager.admin"
```

## Environment

Designed for GCP environments with metadata server access:
- Cloud Run
- Google Compute Engine (GCE)
- Google Kubernetes Engine (GKE)

## Why This Exists

Most projects don't need 90+ dependencies just to read a secret. The official SDK is great if you're using lots of GCP services, but if you just need Secret Manager, this gives you the same functionality with zero deps and a much smaller binary.

## License

MIT

