# tg-rag-ingestor

MVP ingestion system focused on three flows:
- parsing public Telegram channels,
- downloading YouTube audio and auto-transcribing it into RAG-ready text with speaker roles.
- scanning local folders recursively and flattening files from nested archives into one manifest.

## What Is Included

### Telegram
- add source by URL or username,
- sync channel history,
- store raw messages,
- clean/process into documents and chunks,
- inspect data in admin UI.

### YouTube Audio
- add YouTube source by URL,
- trigger audio download with `yt-dlp`,
- auto-transcribe downloaded audio via Deepgram pre-recorded API,
- generate role-marked RAG text and store it as processed document/chunks,
- store audio + transcription metadata in DB,
- preview speaker roles and RAG text in admin UI.

### Local Folder Scan
- provide a folder path that is reachable by the Go API process,
- recurse through nested directories,
- auto-extract supported archives,
- return a single flat manifest of every discovered file,
- keep unsupported archive formats in the output and mark them as skipped.

## Architecture

### Backend (Go)
- HTTP API on `chi`
- Postgres storage (`sources`, `raw_messages`, `documents`, `chunks`, `jobs`, `exports`, `youtube_audio_artifacts` for audio metadata)
- local filesystem scan service for recursive file discovery and archive extraction

### Python sidecar (`services/youtube_audio_service`)
- `POST /api/download-youtube-audio`
- `POST /api/transcribe-youtube-audio`
- `GET /api/download-youtube-audio-file?path=...`

Uses:
- `yt-dlp`
- `ffmpeg` (via yt-dlp extract-audio flow)
- `deepgram-sdk` (pre-recorded transcription + diarization)

### Frontend (React + Vite)
- single simplified navigation (`Sources`)
- source details page for Telegram or YouTube audio artifact

## Quick Start

```bash
cp .env.example .env
docker compose up -d --build
```

Open:
- frontend: `http://localhost:5173`
- api: `http://localhost:18080`
- api health: `http://localhost:18080/health`

## Environment Variables

Core:
- `POSTGRES_DSN`
- `REDIS_ADDR`
- `QDRANT_URL`
- `APP_PORT`
- `CORS_ALLOWED_ORIGIN`
- `MIGRATION_DIR`
- `EXPORT_DIR`
- `DEFAULT_SYNC_BATCH_SIZE`
- `FILESCAN_EXTRACT_DIR` (optional, default `./data/filescan_extracts`)
- `FILESCAN_MAX_FILES` (optional, default `20000`)
- `FILESCAN_MAX_ARCHIVE_DEPTH` (optional, default `4`)

YouTube audio:
- `PY_YOUTUBE_AUDIO_SERVICE_URL` (Go API -> Python audio service)
- `YOUTUBE_AUDIO_STORAGE_DIR`
- `YTDLP_COOKIES_FROM_BROWSER` (optional)
- `YTDLP_COOKIES_FILE` (optional)
- `YTDLP_PLAYER_CLIENT` (optional, default `android`)
- `YTDLP_JS_RUNTIME` (optional)
- `YTDLP_FETCH_METADATA` (optional)
- `YTDLP_AUDIO_FORMAT` (optional, default `mp3`)
- `YTDLP_AUDIO_QUALITY` (optional, default `5`)
- `DEEPGRAM_API_KEY` or `DEEPGRAM_API_KEYS` (required for transcription)
- `DEEPGRAM_API_KEY_FALLBACK` (optional)
- `DEEPGRAM_MODEL` (optional, default `nova-3`)
- `DEEPGRAM_DETECT_LANGUAGE` (optional, default `1`)
- `DEEPGRAM_SMART_FORMAT` (optional, default `1`)
- `DEEPGRAM_PUNCTUATE` (optional, default `1`)
- `DEEPGRAM_PARAGRAPHS` (optional, default `0`)
- `DEEPGRAM_CLIENT_TIMEOUT_SECONDS` (optional, default `900`)
- `DEEPGRAM_REQUEST_TIMEOUT_SECONDS` (optional, default `7200`)
- `DEEPGRAM_SDK_MAX_RETRIES` (optional, default `3`)
- `DEEPGRAM_ATTEMPTS_PER_KEY` (optional, default `3`)
- `DEEPGRAM_RETRY_BASE_SECONDS` (optional, default `2`)
- `DEEPGRAM_RETRY_MAX_SECONDS` (optional, default `30`)

Telegram:
- `TELEGRAM_MODE=stub|mtproto`
- `TELEGRAM_API_ID`
- `TELEGRAM_API_HASH`
- `TELEGRAM_PHONE`
- `TELEGRAM_SESSION_FILE`
- `TELEGRAM_AUTH_CODE`
- `TELEGRAM_PASSWORD`

## API (YouTube)

- `GET /api/youtube/sources`
- `POST /api/youtube/sources`
- `GET /api/youtube/sources/{id}`
- `GET /api/youtube/sources/{id}/audio`
- `POST /api/youtube/sources/{id}/download-audio`
- `POST /api/youtube/sources/{id}/transcribe-audio` (re-run transcription for already downloaded audio)

`download-audio` now runs the full chain:
1. download audio (`yt-dlp`)
2. transcribe + diarize speakers (Deepgram)
3. save RAG-ready document/chunks
4. update artifact metadata and statuses

## API (Filesystem Scan)

- `POST /api/filesystem/scan`

Request:

```json
{
  "path": "/absolute/path/on/api/host"
}
```

Response includes:
- `files[]` with `logical_path`, `resolved_path`, `origin`, and `size_bytes`
- `archives_processed`
- `skipped_archives[]`
- `supported_archive_extensions`

Supported archive formats:
- `.zip`
- `.tar`
- `.tar.gz`
- `.tgz`
- `.tar.bz2`
- `.tbz2`
- `.gz`
- `.bz2`

Recognized but not extracted:
- `.7z`
- `.rar`
- `.xz`
- `.tar.xz`

## Local Commands

```bash
make up
make down
make ps
make logs
make migrate
make api
make frontend
make youtube-service
make backend-build
make frontend-build
make check
```

## Notes

- CPU-first setup is enough.
- If YouTube requires auth/cookies, configure one of the `YTDLP_COOKIES_*` variables.
- Audio is persisted under `YOUTUBE_AUDIO_STORAGE_DIR` and available for direct download via sidecar endpoint.
- Deepgram transcription requires valid API key(s); without them, download works but transcription fails with structured error.
- For long videos, keep compressed extraction (`YTDLP_AUDIO_FORMAT=mp3`) and increase `DEEPGRAM_REQUEST_TIMEOUT_SECONDS` if needed.
- Sidecar image includes `nodejs` for yt-dlp JavaScript extraction; if you run outside Docker, install Node or set `YTDLP_JS_RUNTIME` accordingly.
- Filesystem scan works only for paths visible to the API process. If you run the backend in Docker, mount the host folder into the container first.
- Auto-extracted archive contents are written under `FILESCAN_EXTRACT_DIR`.
