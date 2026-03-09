# AGENTS.md

## Project goal
This repository contains an MVP for ingesting public Telegram channel content, cleaning it, and preparing JSONL output for RAG pipelines.

## Main stack
- Backend: Go
- Frontend: React + TypeScript + Vite
- DB: PostgreSQL
- Queue/Jobs: Redis
- Vector DB scaffold: Qdrant

## Non-negotiable rules
- Keep Telegram integration isolated in its own adapter layer.
- Do not use Telegram Bot API for history collection logic.
- Preserve raw messages separately from processed documents.
- JSONL export is required.
- Exact dedupe is required.
- Frontend must remain simple and maintainable.
- Do not add unnecessary frameworks.
- Do not over-abstract.
- Do not break local run commands.

## MVP must support
- add source
- list sources
- run sync using stub collector
- create raw messages
- clean text
- create processed documents
- create chunks
- preview cleaning/chunking
- export JSONL
- list jobs

## Code style
- Keep package boundaries explicit.
- Prefer small services over god objects.
- Write clear code with minimal but useful comments.
- Use env-based config.
- Make backend compile and frontend build before finishing.

## Definition of done
- backend runs
- frontend runs
- README is complete
- sync using stub collector works
- JSONL export works