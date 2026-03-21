# STRATA Server

Version control built for large binary files. Designed for Unreal Engine projects where Git falls apart.

STRATA uses content-defined chunking to break large files into deduplicated pieces, stores them in object storage, and tracks versions with full commit snapshots. A 78GB project where one texture changes means uploading a few hundred KB — not the whole file.

## What it does

- **Sub-file deduplication** — files are chunked and stored by content hash. Identical chunks across files and versions are stored once.
- **Branching and merging** — create branches, merge with auto-detection of conflicts. Binary files that change on both branches require manual resolution (pick A or B). Different files auto-merge cleanly.
- **File locking** — advisory locks prevent two people from editing the same .umap simultaneously. Locks are enforced on commit.
- **Full version history** — every commit is a complete snapshot. Check out any commit at any time without walking the history chain.
- **Proxied storage** — clients never talk to MinIO directly. The server handles auth, hash verification, and chunk routing.

## Architecture

```
┌───────────┐  ┌───────────┐  ┌──────────┐
│  C++ CLI  │  │ UE Plugin │  │ React UI │
└────┬──────┘  └────┬──────┘  └────┬─────┘
     │              │              │
     └──────────────┼──────────────┘
                    │
            ┌───────┴──────┐
            │  Go Server   │  :8080
            │  (chi + JWT) │
            └──┬────────┬──┘
               │        │
        ┌──────┴───┐ ┌──┴────────┐
        │PostgreSQL│ │   MinIO   │
        │ metadata │ │chunk blobs│
        └──────────┘ └───────────┘
```

- **Go server** — HTTP API, auth, chunk proxy, merge algorithm
- **PostgreSQL** — users, projects, commits, file versions, manifests, locks
- **MinIO** — S3-compatible object storage for raw chunk bytes

## Quick start

```bash
# Clone and start everything
docker compose up --build

# Verify
curl http://localhost:8080/health
# → {"status":"ok","service":"strata"}
```

This starts PostgreSQL (:5432), MinIO (:9000, console at :9001), runs migrations automatically, and starts the STRATA server on :8080.

### First steps

```bash
# Register
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"eli","email":"eli@example.dev","password":"changeme123"}'

# Login
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"eli","password":"changeme123"}'
# → {"access_token":"eyJ...","refresh_token":"eyJ...","expires_in":900}

# Create a project
curl -X POST http://localhost:8080/api/v1/projects \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <access_token>" \
  -d '{"name":"my-ue-project","description":"Unreal Engine project"}'
```

## API overview

All endpoints are under `/api/v1`. Auth endpoints are public; everything else requires `Authorization: Bearer <token>`.

| Area | Endpoints | Description |
|------|-----------|-------------|
| Auth | `POST /auth/register, login, refresh, logout` | JWT access + refresh tokens |
| Projects | `POST/GET/DELETE /projects` | Create, list, get, delete |
| Push | `POST /push/check, /push/chunk, /push/commit` | Upload chunks, create commits |
| Pull | `POST /pull/diff`, `GET /pull/chunk/{hash}` | Diff versions, download chunks |
| Branches | `POST /branches`, `GET /refs` | Create branches, list refs |
| Merge | `POST /merge/analyze, /merge` | Preview and execute merges |
| Locks | `GET/POST/DELETE /locks` | File locking |
| History | `GET /commits, /commits/{id}, /tree` | Browse commits and file trees |

## Project structure

```
strata-server/
├── cmd/server/main.go            # Entry point, wiring, router
├── internal/
│   ├── config/                   # Environment config loader
│   ├── handler/                  # HTTP handlers (auth, push, pull, merge, lock)
│   ├── middleware/               # JWT auth, logging, rate limiting
│   ├── model/                    # Data types (user, project, commit, chunk, lock)
│   ├── service/                  # Business logic (auth, chunk, version, merge, lock)
│   └── store/
│       ├── postgres/             # Database queries
│       └── minio/                # Chunk blob storage
├── migrations/                   # SQL migrations (auto-applied by Docker)
├── docker-compose.yml
├── Dockerfile
└── go.mod
```

## Configuration

All config is via environment variables. See `docker-compose.yml` for the full list. Key ones:

| Variable | Default | Description |
|----------|---------|-------------|
| `STRATA_PORT` | 8080 | Server port |
| `STRATA_DB_HOST` | localhost | PostgreSQL host |
| `STRATA_DB_PASSWORD` | — | PostgreSQL password (required) |
| `STRATA_MINIO_ENDPOINT` | localhost:9000 | MinIO endpoint |
| `STRATA_JWT_SECRET` | — | JWT signing key (required, min 32 chars) |
| `STRATA_MAX_CHUNK_SIZE` | 4194304 | Max chunk size in bytes (4MB) |

## How pushing works

1. Client chunks the file locally (content-defined chunking, ~1MB average)
2. Client computes SHA-256 for each chunk
3. `POST /push/check` — server says which chunks it already has
4. Client uploads only the new chunks (16-32 concurrent)
5. `POST /push/commit` — server records the manifest and advances the branch

## How merging works

STRATA uses file-level merge resolution for binary assets:

- **Fast-forward** — target branch hasn't changed. Just moves the pointer, no merge commit.
- **Clean merge** — branches modified different files. Combined automatically.
- **Conflict** — same file modified on both branches. Client must pick one version per conflicted file.

This matches how Perforce handles binary merges in game studios — the difference is STRATA gives you sub-file deduplication on storage.

## Development

```bash
# Run without Docker (need local PostgreSQL + MinIO)
export STRATA_DB_PASSWORD=yourpass
export STRATA_JWT_SECRET=$(openssl rand -hex 32)
go run ./cmd/server

# Run migrations manually
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
migrate -path migrations -database "postgres://..." up
```

## License

MIT