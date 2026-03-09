# Architecture Notes

## Pipeline

1. Source is added (`sources`).
2. Sync is triggered (creates `jobs` row).
3. Telegram collector fetches message batch.
4. Raw payloads are upserted into `raw_messages`.
5. Processing creates/updates `documents` and `chunks`.
6. Job progress and status are updated in `jobs`.
7. JSONL export creates `exports` record and writes file.

## Boundaries

- `internal/telegram`:
  - source resolution + history fetch API.
  - `stub` implementation for local MVP.
  - `mtproto` skeleton implementation for future integration.
- `internal/processing`:
  - normalization, filters, dedupe, chunking orchestration.
- `internal/storage`:
  - SQL repository and data mapping.
- `internal/export`:
  - JSONL generation and export tracking.

## Persistence Model

- `sources`: source lifecycle and sync state.
- `raw_messages`: immutable source payload + metadata snapshot.
- `documents`: cleaned/normalized content and dedupe flags.
- `chunks`: RAG-ready text units.
- `jobs`: operational sync progress.
- `exports`: export history and artifact paths.
