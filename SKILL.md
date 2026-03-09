# Codex Skill: Telegram Channel Ingestion Pipeline

## Purpose
This skill guides Codex to build and modify a clean ingestion-first system for parsing large public Telegram channels, cleaning and normalizing the content, storing operational metadata, and exporting a high-quality corpus to JSONL for later RAG ingestion.

The project must **not** start with embeddings. The first milestone is a reliable, observable, restart-safe ingestion pipeline that produces a clean corpus. Only after the cleaned corpus quality is acceptable should embeddings and vector indexing be added.

## Project Stack
- **Backend:** Go
- **Telegram client:** `gotd/td`
- **HTTP API:** `chi`
- **PostgreSQL access:** `pgx`
- **Database:** PostgreSQL
- **Cache / queues / locks:** Redis
- **Frontend:** React + Vite + TypeScript + MUI
- **Vector DB:** Qdrant
- **Export format:** JSONL

## Core Product Principle
Build the system in phases:

1. **Phase 1: Ingestion pipeline only**
   - collect messages from public Telegram channels
   - persist raw message payloads and metadata
   - clean and normalize text
   - deduplicate and structure output
   - export clean corpus to JSONL
   - add admin UI for jobs, channels, logs, and export state

2. **Phase 2: Corpus quality hardening**
   - improve cleaners
   - normalize entities, links, mentions, duplicates, media captions
   - define chunking strategy
   - validate JSONL quality

3. **Phase 3: Embeddings and retrieval**
   - only after cleaned corpus is stable
   - generate embeddings from clean exported chunks
   - push vectors into Qdrant
   - expose search / retrieval endpoints

Do **not** prematurely couple ingestion to embeddings.

## What Codex Should Optimize For
- clean architecture
- resumable ingestion for large channels
- idempotent processing
- observability and debugging
- simple local development via Docker Compose
- explicit separation of raw data, cleaned data, and exported data
- future-safe integration with embeddings and Qdrant

## Required Architecture
Use a modular monorepo-ish structure:

```text
project-root/
  backend/
    cmd/api/
    cmd/worker/
    internal/
      app/
      config/
      http/
      telegram/
      ingestion/
      cleaning/
      export/
      storage/
      queue/
      models/
      observability/
  frontend/
    src/
      app/
      pages/
      components/
      features/
      api/
      types/
  deploy/
    docker-compose.yml
  docs/
  SKILL.md
```

## Backend Responsibilities
### API service
Build endpoints for:
- channel management
- ingestion job creation / restart / stop
- job status and progress
- cleaned corpus inspection
- export-to-JSONL workflows
- system health

### Worker service
Build a worker that:
- fetches Telegram messages in batches
- stores raw payloads
- pushes messages through cleaning pipeline
- writes normalized records
- schedules retries on transient failure
- supports resumable progress by channel and message range

### Storage layers
Keep these logical layers separate:
- `raw_messages`
- `clean_messages`
- `exports`
- `ingestion_jobs`
- `channels`

### Redis usage
Use Redis for:
- job queueing
- lightweight distributed locks
- rate-limit backoff / retry scheduling
- ephemeral job progress cache

## Data Flow
The ingestion flow should be:

1. user adds public Telegram channel
2. backend validates and registers channel
3. worker creates ingestion job
4. worker fetches messages via `gotd/td`
5. raw message payloads stored in Postgres
6. cleaning pipeline transforms messages into normalized records
7. deduplication and filtering applied
8. clean records stored in Postgres
9. export service writes JSONL from clean records
10. later, separate embedding pipeline consumes JSONL or clean records

## Cleaning Rules
Design the cleaning pipeline as composable steps. Each step should be testable in isolation.

Suggested stages:
- remove empty / deleted / service-only messages
- extract plain text from message body and caption
- normalize whitespace and Unicode
- strip tracking params from URLs when safe
- preserve important links in structured fields
- keep source metadata: channel, message id, timestamp, link, reply info
- flag language if easy to add later, but do not block on it
- mark forwards / reposts / duplicates
- remove obvious boilerplate if configured per channel
- preserve auditability by linking clean record to raw record

Do not destroy provenance. Every clean record should point back to its raw source.

## JSONL Export Schema
Default JSONL schema should be stable and RAG-friendly:

```json
{
  "id": "telegram:<channel_id>:<message_id>",
  "channel_id": "...",
  "channel_name": "...",
  "message_id": 123,
  "date": "2026-03-09T10:00:00Z",
  "text": "cleaned message text",
  "source_url": "https://t.me/...",
  "reply_to_message_id": 122,
  "forwarded": false,
  "has_media": true,
  "media_type": "photo",
  "raw_record_id": "uuid",
  "cleaning_version": "v1",
  "tags": ["telegram", "channel_post"]
}
```

Keep the export schema explicit and versioned.

## Database Guidance
Use Postgres migrations. Suggested tables:
- `channels`
- `ingestion_jobs`
- `ingestion_checkpoints`
- `raw_messages`
- `clean_messages`
- `export_jobs`
- `export_files`

Important constraints:
- unique `(channel_id, telegram_message_id)` where appropriate
- timestamps on all major entities
- status enums or constrained strings for jobs
- checkpoint table for resumability

## Frontend Scope
Build an admin UI, not a marketing site.

Pages:
- Channels
- Ingestion Jobs
- Clean Corpus
- Exports
- System Health

Use React + Vite + TypeScript + MUI.
Focus on utility:
- tables
- filters
- status chips
- progress bars
- job logs
- retry / restart actions
- export download list

Do not over-design. The UI exists to inspect and control the pipeline.

## API Design Expectations
Prefer simple REST endpoints:
- `POST /api/channels`
- `GET /api/channels`
- `POST /api/ingestion/jobs`
- `GET /api/ingestion/jobs`
- `GET /api/ingestion/jobs/:id`
- `POST /api/ingestion/jobs/:id/retry`
- `GET /api/clean-messages`
- `POST /api/exports/jsonl`
- `GET /api/exports`
- `GET /api/health`

Return structured JSON with consistent error format.

## Telegram-specific Notes
Use `gotd/td` carefully:
- respect Telegram limits
- make retries explicit
- persist checkpoints often for large channels
- support continuation after worker restarts
- keep channel resolution and message fetch logic isolated behind interfaces

Do not hardwire Telegram logic into handlers.

## Testing Priorities
Write tests for:
- cleaning pipeline
- deduplication logic
- export schema generation
- repository logic where practical
- API handlers for core flows

At minimum include fixture-based tests for messy Telegram messages.

## Observability
Include:
- structured logs
- job progress metrics
- error counters
- traceable job ids
- health endpoint

Human beings love building pipelines they cannot debug, then acting surprised. Do the opposite.

## Local Development Requirements
Provide Docker Compose for:
- postgres
- redis
- qdrant (installed now for later phases, even if unused initially)
- backend api
- backend worker
- frontend

Include:
- `.env.example`
- migration instructions
- seed / demo instructions if possible

## Rules for Codex When Generating Code
1. Prefer small, composable packages over giant files.
2. Keep interfaces near the domain that uses them.
3. Separate transport models from database models.
4. Use context propagation properly in Go.
5. Use `pgx` directly or through a thin repository layer.
6. Make retries and backoff explicit.
7. Do not add embeddings until ingestion and export are working.
8. Do not mix Qdrant integration into the first implementation path.
9. Document assumptions in `docs/architecture.md`.
10. Keep the system runnable locally without cloud dependencies.

## First Implementation Goal
Codex should generate a working MVP with:
- backend API in Go
- worker in Go
- Postgres schema and migrations
- Redis-backed job orchestration
- Telegram ingestion skeleton with `gotd/td`
- cleaning pipeline v1
- JSONL export
- React admin UI for channels, jobs, and exports
- Docker Compose for local run

## Definition of Done for Phase 1
Phase 1 is done only when:
- a public channel can be registered
- ingestion job can run and resume
- raw and clean records are persisted
- cleaned corpus can be browsed in UI
- JSONL export can be generated
- exported data is suitable for later embedding

## Explicit Non-Goals for Phase 1
Do not spend time first on:
- semantic search
- RAG answers
- embeddings generation
- reranking
- LLM chat UI
- advanced analytics dashboards

Humans always want the shiny retrieval demo before they have usable data. Resist that impulse.

## Future Phase Hook
After Phase 1 and corpus validation, add:
- chunking strategy
- embedding worker
- Qdrant collections
- indexing status tracking
- retrieval API
- search UI

But not before the corpus is genuinely clean.

## Output Style for Codex
When acting under this skill, Codex should:
- explain architectural decisions briefly
- generate production-minded code, not toy snippets
- prefer complete files when asked
- preserve consistency with the declared stack
- avoid introducing extra frameworks unless clearly justified
