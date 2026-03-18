# AGENTS.md

## Project purpose
This repository contains an ingestion utility for:
- parsing public Telegram channels,
- cleaning and normalizing text,
- preparing RAG-ready documents and chunks,
- exporting JSONL,
- and now adding diarized YouTube transcription by URL.

## New YouTube diarization feature
A YouTube URL must go through this pipeline:
1. audio download via `yt-dlp`
2. ASR via `WhisperX`
3. speaker diarization via `pyannote.audio`
4. transcript merge into ordered segments
5. storage in DB
6. processing into document/chunk pipeline
7. JSONL export
8. preview and role mapping in the React admin UI

## Non-negotiable terminology
These concepts must NEVER be mixed:

- `speaker_label`:
  diarization output such as `SPEAKER_00`, `SPEAKER_01`
- `role_label`:
  semantic role assigned later, such as `host`, `guest`, `doctor`, `expert`

Rules:
- diarization labels are not semantic roles by default
- do not auto-claim speaker identity
- do not auto-claim host/guest unless a mapping exists
- if no mapping exists, `role_label` remains null
- UI, API, DB schema, docs, and code must keep this distinction explicit

## Architectural rules
- Keep the main backend in Go.
- Put YouTube transcription/diarization logic in a dedicated Python sidecar service.
- Keep Telegram logic isolated and do not regress existing Telegram flows.
- Do not rewrite the project from scratch.
- Implement incrementally on top of the current repository.
- Reuse the existing processing pipeline where appropriate.
- Preserve raw data separately from processed data.
- Keep source-specific logic isolated.

## Required stack for YouTube feature
- `yt-dlp` for downloading audio from YouTube
- `WhisperX` for ASR and timestamps
- `pyannote.audio` for speaker diarization
- Python sidecar service for ML/media pipeline
- Go adapter layer to call the Python service
- React UI for source creation, transcript preview, and role mapping

## What must be stored
At minimum store:
- YouTube source record
- transcript artifact
- transcript segments
- optional speaker-to-role mapping
- processed document(s)
- processed chunks
- jobs and statuses

Transcript segments must preserve:
- `start_seconds`
- `end_seconds`
- `speaker_label`
- `role_label` nullable
- `text_raw`
- `text_clean` if available

## Data model expectations
For the YouTube feature, the repository should include schema support for:
- source_type = `youtube_video`
- provider = `whisperx_pyannote`
- transcript artifacts
- transcript segments
- optional speaker role mappings

## API expectations
Expected backend capabilities:
- add YouTube source by URL
- trigger transcription job
- fetch transcript artifact
- fetch transcript segments
- save speaker-role mappings
- fetch processed documents
- export JSONL

## UI expectations
The React admin UI must support:
- adding YouTube URLs
- starting transcription
- transcript preview
- diarized segment table
- manual role mapping
- processed document preview
- chunk preview
- export action

The UI must clearly show:
- provider
- source status
- speaker labels
- role labels
- whether role mappings were applied

## Performance and execution constraints
- A CPU-first local MVP is acceptable.
- GPU acceleration may be optional.
- If GPU is unavailable, handle fallback cleanly where possible.
- Return structured errors for yt-dlp, ffmpeg, WhisperX, pyannote, and service connectivity failures.
- Clean up temp audio files when appropriate.

## Code quality rules
- Prefer a working MVP over over-abstraction.
- Keep package boundaries explicit.
- Use typed request/response structs where appropriate.
- Keep comments useful, not noisy.
- Avoid giant god-objects.
- Do not break the existing Telegram ingestion flow.
- Do not invent fake diarization or fake semantic roles.

## Definition of done
The feature is complete only when:
- a user can add a YouTube source by URL
- the backend triggers the Python sidecar
- audio is downloaded with `yt-dlp`
- transcription is produced with `WhisperX`
- diarization is produced with `pyannote.audio`
- merged segments are stored as `start/end/speaker_label/text`
- frontend displays transcript segments
- manual role mapping works
- processed document(s) are created
- chunks are created
- JSONL export works
- README explains setup, env vars, dependencies, and limitations

## Final reporting requirements for Codex
At the end of implementation, report:
- what was implemented
- schema changes
- API endpoints added
- new environment variables
- local run commands
- CPU/GPU limitations
- reminder that `speaker_label` is not the same as `role_label`
