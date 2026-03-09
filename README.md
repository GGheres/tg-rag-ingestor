# tg-rag-ingestor

MVP ingestion system for public Telegram channels:
- adds Telegram sources (URL or username),
- syncs channel history page-by-page until full history is fetched,
- stores raw messages and processed documents,
- chunks text for RAG ingestion,
- exports JSONL,
- provides a React admin UI.

## Architecture

### Backend (Go)
- HTTP API: `chi`
- Storage: PostgreSQL (`sources`, `raw_messages`, `documents`, `chunks`, `jobs`, `exports`)
- Queue/status: job records in Postgres (sync runs inline in MVP)
- Telegram adapter:
  - `stub` mode (fully working for local MVP),
  - `mtproto` mode via `gotd/td` for real public channels

### Frontend (React + Vite + TS)
- Admin pages:
  - Sources
  - Source Details
  - Jobs
  - Preview (cleaner/chunker)
  - Exports

### Infrastructure
- `docker-compose` for:
  - PostgreSQL
  - Redis
  - Qdrant (scaffold for future embedding/index phase)

## Repository Layout

```text
backend/
  cmd/api
  cmd/collector
  cmd/migrate
  cmd/worker
  internal/
    api/
    app/
    chunking/
    cleaning/
    config/
    db/
    export/
    ingestion/
    models/
    processing/
    storage/
    telegram/
  migrations/
frontend/
  src/
    api/
    components/
    pages/
docs/
docker-compose.yml
.env.example
Makefile
```

## Local Run

### 1) Prepare environment

```bash
cp .env.example .env
```

`TELEGRAM_MODE=stub` is default and gives a fully working local sync flow.

For real channels, set:
- `TELEGRAM_MODE=mtproto`
- `TELEGRAM_API_ID`
- `TELEGRAM_API_HASH`
- `TELEGRAM_PHONE`
- `TELEGRAM_SESSION_FILE`

First MTProto authorization requires one-time code in `TELEGRAM_AUTH_CODE`.
If the account has 2FA enabled, also set `TELEGRAM_PASSWORD`.
After successful login, the session is saved to `TELEGRAM_SESSION_FILE` and code is no longer required.
If `TELEGRAM_AUTH_CODE` is empty and API is started from terminal, the backend will prompt for the code interactively.

### 2) Start infra

```bash
make up
```

### 3) Run backend

```bash
make api
```

Backend runs on `http://localhost:8080`.
Migrations are auto-applied on API startup.

Optional manual migration:

```bash
make migrate
```

### 4) Run frontend

```bash
make frontend
```

Frontend runs on `http://localhost:5173`.

## API Endpoints (MVP)

- `GET /health`
- `GET /api/sources`
- `POST /api/sources`
- `POST /api/imports/json`
- `GET /api/sources/:id`
- `POST /api/sources/:id/sync`
- `GET /api/sources/:id/raw-messages`
- `GET /api/sources/:id/documents`
- `POST /api/posts/parsed`
- `GET /api/documents/:id`
- `GET /api/documents/:id/download?format=txt|json`
- `POST /api/preview/clean`
- `POST /api/exports/jsonl`
- `GET /api/exports`
- `GET /api/exports/:id/download`
- `GET /api/jobs`

`POST /api/posts/parsed` request body:
- `source_ids`: array of source UUIDs (required)
- `limit_per_source`: max posts per selected channel (default 100, max 2000)
- `include_duplicates`: include duplicate documents in response (default `false`)

`POST /api/imports/json` multipart form:
- `file`: JSON file (required, up to 100MB)
- `title`: optional source title for the imported dataset

## Core MVP Flow

1. Add source (`/api/sources`) with URL or username.
2. Trigger sync (`/api/sources/:id/sync`).
   The sync runs by pages and fetches full available history by default.
   Optional body:
   - `batch_size` (default from `DEFAULT_SYNC_BATCH_SIZE`)
   - `max_messages` (0 = no cap, full history)
   - `full_resync` (`true` ignores `last_message_id` and re-fetches full history from the beginning)
3. Collector mode:
   - `stub`: realistic Telegram-like synthetic messages.
   - `mtproto`: real `messages.getHistory` pagination until history end.
4. Raw payloads are stored in `raw_messages`.
5. Processing pipeline:
   - text normalization,
   - link/hashtag/mention extraction,
   - trash filtering,
   - exact duplicate detection by `content_hash`,
   - chunking.
6. Processed docs/chunks are stored.
7. Export JSONL from documents or chunks via `/api/exports/jsonl`.

## JSONL Export

Export modes:
- `documents` (default)
- `chunks`

Export formats:
- `jsonl` (default)
- `txt_rag` (documents only, one `.txt` file with RAG block markers and metadata)

`POST /api/exports/jsonl` body supports:
- `source_id`: optional UUID (omit for all sources)
- `mode`: `documents` or `chunks`
- `format`: `jsonl` or `txt_rag`
- `include_duplicates`: include duplicate documents/chunks in export (default `false`)
- `include_trash`: include trash raw messages that were filtered out from processed documents (default `false`)

Generated files are saved to `EXPORT_DIR` (default: `./data/exports`).
Ready exports can be downloaded via `GET /api/exports/:id/download`.

Processed document download:
- `GET /api/documents/:id/download?format=txt`
- `GET /api/documents/:id/download?format=json`

Example JSONL entry:

```json
{
  "doc_id": "tg:somechannel:12345",
  "text": "normalized text",
  "metadata": {
    "source": "telegram",
    "source_subtype": "public_channel_post",
    "channel_username": "somechannel",
    "message_id": 12345,
    "url": "https://t.me/somechannel/12345",
    "published_at": "2026-03-09T10:30:00Z",
    "language": "ru",
    "is_forward": false,
    "quality_score": 0.91
  }
}
```

## Config

See [.env.example](/Users/gg/tg-rag-ingestor/.env.example) for full list.

Main variables:
- `POSTGRES_DSN`
- `REDIS_ADDR`
- `QDRANT_URL`
- `APP_PORT`
- `CORS_ALLOWED_ORIGIN`
- `MIGRATION_DIR`
- `EXPORT_DIR`
- `TELEGRAM_MODE=stub|mtproto`
- `TELEGRAM_API_ID`
- `TELEGRAM_API_HASH`
- `TELEGRAM_PHONE`
- `TELEGRAM_SESSION_FILE`
- `TELEGRAM_AUTH_CODE` (one-time, first login)
- `TELEGRAM_PASSWORD` (only if 2FA is enabled)
- `DEFAULT_SYNC_BATCH_SIZE`

## Commands

```bash
make up
make down
make ps
make logs
make migrate
make api
make worker
make collector
make frontend
make backend-build
make frontend-build
make check
```

## Known Limitations (MVP)

- First MTProto login through API is terminal-oriented; interactive prompt requires running API in a terminal TTY.
- Jobs are stored with progress in Postgres and executed inline on sync endpoint.
- Embeddings and Qdrant indexing are not implemented yet (Qdrant is scaffolded).
- Near-duplicate detection is scaffolded via `simhash` placeholder.

## Next Steps

1. Move sync execution to dedicated async worker queue (Redis-backed).
2. Add reprocess endpoint and per-source cleaning presets.
3. Add embedding worker + Qdrant upsert flow.
4. Add integration tests for sync/processing/export pipeline.
5. Add robust interactive auth bootstrap flow (CLI or web) for MTProto.
