# tg-rag-ingestor

MVP ingestion system focused on three flows:
- parsing public Telegram channels,
- downloading YouTube audio and auto-transcribing it into RAG-ready text with speaker roles.
- extracting all candidate resumes for a specific HH vacancy (employer OAuth2 API, partial success aware).
- scanning local folders recursively and flattening files from nested archives into one manifest.

## What Is Included

### Telegram
- add source by URL or username,
- sync channel history,
- add a channel-level source from one Telegram channel URL,
- auto-enumerate message links from the channel history,
- add a source by a list of Telegram message links,
- fetch private `t.me/c/...` channel messages by direct links,
- store raw messages,
- clean/process messages into documents and chunks,
- merge a whole channel history into one RAG document,
- merge curated message-link sets into one RAG document,
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

### HeadHunter Employer Resume Pool
- OAuth2 URL generation + authorization-code exchange support,
- extraction by `vacancy_id` through `/negotiations` and `collections[].url`,
- deduplication by `resume_id`,
- fetch full resume per candidate,
- optional download of original PDF/RTF (if API access allows),
- combined `combined_resumes.md` and `combined_resumes.html`,
- optional `combined_resumes.pdf` via headless Chrome/Chromium,
- `manifest.json` with per-candidate status and partial success semantics,
- admin UI page for run/monitor/download (`HH Resumes` tab).

## Architecture

### Backend (Go)
- HTTP API on `chi`
- Postgres storage (`sources`, `raw_messages`, `documents`, `chunks`, `jobs`, `exports`, `telegram_message_links`, `youtube_audio_artifacts` for audio metadata)
- local filesystem scan service for recursive file discovery and archive extraction
- HH integration modules:
  - `internal/auth` (OAuth2 code exchange/refresh),
  - `internal/hhclient` (retry/backoff/rate-limit/error mapping),
  - `internal/negotiations` (vacancy negotiations + collection traversal),
  - `internal/resumes` (resume fetch + original download),
  - `internal/export` (combined HTML/MD/PDF + manifest),
  - `internal/service` (orchestration and partial success flow),
  - `internal/model` (HH models + filesystem abstraction).

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
- dedicated `HH Resumes` page for OAuth exchange, extraction launch, manifest preview, and file downloads

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
- `MISTRAL_API_KEY` (required for local-folder RAG export)
- `MISTRAL_API_BASE_URL` (optional, default `https://api.mistral.ai`)
- `MISTRAL_OCR_MODEL` (optional, default `mistral-ocr-latest`)
- `MISTRAL_OCR_TIMEOUT_SECONDS` (optional, default `900`)
- `MISTRAL_OCR_RETRY_ATTEMPTS` (optional, default `4`)
- `MISTRAL_OCR_RETRY_BASE_MS` (optional, default `1200`)

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
- `TELEGRAM_MODE=mtproto` (required, fail-fast if missing or different)
- `TELEGRAM_API_ID`
- `TELEGRAM_API_HASH`
- `TELEGRAM_PHONE`
- `TELEGRAM_SESSION_FILE`
- `TELEGRAM_AUTH_CODE`
- `TELEGRAM_PASSWORD`

HeadHunter:
- `HH_CLIENT_ID`
- `HH_CLIENT_SECRET`
- `HH_ACCESS_TOKEN` (optional if refresh token is available)
- `HH_REFRESH_TOKEN` (optional if access token is provided)
- `HH_REDIRECT_URI` (OAuth callback URI configured in HH app)
- `HH_USER_AGENT` (required by HH API, also sent as `HH-User-Agent`)
- `HH_OUTPUT_DIR` (default `./data/hh_extractions`)
- `HH_BASE_URL` (default `https://api.hh.ru`)
- `HH_TOKEN_URL` (default `https://api.hh.ru/token`)
- `HH_AUTH_URL` (default `https://hh.ru/oauth/authorize`)
- `HH_REQUEST_TIMEOUT_SEC` (default `30`)
- `HH_RATE_LIMIT_RPS` (default `5`)
- `HH_RETRY_MAX` (default `4`)
- `HH_RETRY_BASE_BACKOFF_MS` (default `400`)
- `HH_RETRY_MAX_BACKOFF_MS` (default `15000`)
- `HH_CONCURRENCY` (default `4`)
- `HH_SAVE_ORIGINALS_DEFAULT` (default `1`)

Private Telegram message-link ingestion requires:
- `TELEGRAM_MODE=mtproto`
- an authorized Telegram session with access to the target private channel
- message links in the same channel, one per line, typically in `https://t.me/c/<channel_id>/<message_id>` format

Private Telegram channel-document ingestion requires:
- `TELEGRAM_MODE=mtproto`
- an authorized Telegram session that already has access to the target channel
- either a public channel URL like `https://t.me/channelname`
- or an invite URL like `https://t.me/+inviteHash`

## API (Telegram)

- `POST /api/sources`
- `POST /api/sources/telegram-channel-document`
- `POST /api/sources/telegram-message-links`
- `GET /api/sources/{id}`
- `POST /api/sources/{id}/sync`
- `GET /api/sources/{id}/raw-messages`
- `GET /api/sources/{id}/message-links`
- `GET /api/sources/{id}/documents`

`POST /api/sources/telegram-channel-document` request:

```json
{
  "title": "Closed channel full document",
  "url": "https://t.me/+inviteHash"
}
```

Behavior:
- sync resolves the channel from the URL,
- fetches the channel history,
- auto-builds canonical message links for every fetched post,
- stores those links in `telegram_message_links`,
- merges cleaned message text into one RAG document.

`POST /api/sources/telegram-message-links` request:

```json
{
  "title": "Closed channel selection",
  "message_links": [
    "https://t.me/c/1941234567/10",
    "https://t.me/c/1941234567/11"
  ]
}
```

Behavior:
- all links must belong to the same Telegram channel,
- links are deduplicated by canonical URL,
- sync fetches only those message IDs instead of the full history,
- cleaned text from all fetched messages is merged into one RAG document,
- per-message raw records are still stored separately.

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

## API (HeadHunter Employer)

- `GET /api/hh/config`
- `POST /api/hh/oauth/exchange`
- `POST /api/hh/extract`
- `GET /api/hh/extractions`
- `GET /api/hh/extractions/{extractionID}`
- `GET /api/hh/extractions/{extractionID}/files/{filename}`

`POST /api/hh/extract` request:

```json
{
  "vacancy_id": "12345678",
  "dry_run": false,
  "export_pdf": false,
  "save_originals": true
}
```

Result files per extraction:
- `manifest.json`
- `combined_resumes.md`
- `combined_resumes.html`
- optional `combined_resumes.pdf`
- optional `originals/*.pdf|*.rtf`

`manifest.json` candidate row fields:
- `candidate_id`
- `resume_id`
- `fio`
- `status` (`ok|partial|error|skipped`)
- `downloaded_original`
- `included_in_combined`
- `error_message`
- `original_file`

Critical note:
- `partial success` mode is enabled by default: unavailable resumes never block successful candidates from being exported.
- Paid-access limitations from HH are surfaced in per-candidate `error_message` (for example `no_available_service`, `cant_view_contacts`).

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

# HH CLI extraction
cd backend && go run ./cmd/hhresumes --vacancy=12345678 --save-originals
```

## Notes

- CPU-first setup is enough.
- If YouTube requires auth/cookies, configure one of the `YTDLP_COOKIES_*` variables.
- Private Telegram message-link sync works only when the configured Telegram account can open the linked posts.
- Private Telegram channel-document sync does not auto-join channels. If you pass an invite link, the configured Telegram account must already be in that channel.
- Audio is persisted under `YOUTUBE_AUDIO_STORAGE_DIR` and available for direct download via sidecar endpoint.
- Deepgram transcription requires valid API key(s); without them, download works but transcription fails with structured error.
- For long videos, keep compressed extraction (`YTDLP_AUDIO_FORMAT=mp3`) and increase `DEEPGRAM_REQUEST_TIMEOUT_SECONDS` if needed.
- Sidecar image includes `nodejs` for yt-dlp JavaScript extraction; if you run outside Docker, install Node or set `YTDLP_JS_RUNTIME` accordingly.
- Filesystem scan works only for paths visible to the API process. If you run the backend in Docker, mount the host folder into the container first.
- Auto-extracted archive contents are written under `FILESCAN_EXTRACT_DIR`.
- Local-folder RAG export now routes text extraction through Mistral OCR for every file type; local PDF/text fallback extractors are disabled.
- Legacy `.doc` files with text/html payload are auto-converted to temporary `.docx` before Mistral OCR, and OCR calls retry on transient network errors.
- HH employer API endpoints can return `403/404/429`, `quota_exceeded`, `no_available_service`, and `cant_view_contacts`; extractor handles retries/backoff/rate-limit and preserves partial results.
- HH resume originals (PDF/RTF) may be unavailable for specific accounts/tariffs even if metadata is available.
