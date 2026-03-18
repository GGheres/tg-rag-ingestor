from __future__ import annotations

import json
import logging
import mimetypes
import os
import re
import shutil
import subprocess
import tempfile
import time
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, urlparse

from fastapi import FastAPI, Query
from fastapi.responses import FileResponse, JSONResponse
from pydantic import BaseModel, Field


app = FastAPI(title="youtube_audio_service", version="0.1.0")
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("uvicorn.error")


class ServiceError(Exception):
    def __init__(
        self,
        *,
        stage: str,
        code: str,
        message: str,
        status_code: int = 400,
        details: dict[str, Any] | None = None,
    ) -> None:
        self.stage = stage
        self.code = code
        self.message = message
        self.status_code = status_code
        self.details = details or {}
        super().__init__(message)


class DownloadAudioRequest(BaseModel):
    source_id: str = Field(..., min_length=1)
    url: str = Field(..., min_length=8)


class DownloadAudioResponse(BaseModel):
    source_id: str
    video_id: str
    title: str | None = None
    audio_file_path: str
    metadata: dict[str, Any]


class TranscribeAudioRequest(BaseModel):
    source_id: str = Field(..., min_length=1)
    audio_file_path: str = Field(..., min_length=1)
    video_id: str | None = None
    language: str | None = None


class TranscribeSegment(BaseModel):
    segment_index: int
    start_seconds: float
    end_seconds: float
    speaker_label: str
    role_label: str
    text_raw: str
    confidence: float | None = None


class TranscribeAudioResponse(BaseModel):
    source_id: str
    video_id: str | None = None
    provider: str
    model: str
    language: str | None = None
    full_text_raw: str
    full_text_rag: str
    speaker_roles: dict[str, str]
    segments: list[TranscribeSegment]
    metadata: dict[str, Any]


YOUTUBE_ID_RE = re.compile(r"^[A-Za-z0-9_-]{6,32}$")
SOURCE_ID_RE = re.compile(r"^[A-Za-z0-9_.:-]{6,128}$")


def parse_bool_env(name: str, default: bool) -> bool:
    raw = os.environ.get(name, "").strip().lower()
    if raw == "":
        return default
    return raw in {"1", "true", "yes", "on"}


def parse_int_env(name: str, default: int, *, min_value: int | None = None, max_value: int | None = None) -> int:
    raw = os.environ.get(name, "").strip()
    if raw == "":
        return default
    try:
        value = int(raw)
    except ValueError:
        logger.warning("%s=%r is not a valid integer; using default=%s", name, raw, default)
        return default
    if min_value is not None and value < min_value:
        logger.warning("%s=%s is below min=%s; using min", name, value, min_value)
        value = min_value
    if max_value is not None and value > max_value:
        logger.warning("%s=%s is above max=%s; using max", name, value, max_value)
        value = max_value
    return value


def parse_float_env(name: str, default: float, *, min_value: float | None = None, max_value: float | None = None) -> float:
    raw = os.environ.get(name, "").strip()
    if raw == "":
        return default
    try:
        value = float(raw)
    except ValueError:
        logger.warning("%s=%r is not a valid float; using default=%s", name, raw, default)
        return default
    if min_value is not None and value < min_value:
        logger.warning("%s=%s is below min=%s; using min", name, value, min_value)
        value = min_value
    if max_value is not None and value > max_value:
        logger.warning("%s=%s is above max=%s; using max", name, value, max_value)
        value = max_value
    return value


def normalize_language(value: str | None) -> str | None:
    if value is None:
        return None
    cleaned = value.strip().lower()
    if cleaned == "":
        return None
    return cleaned


def resolve_deepgram_api_keys() -> list[str]:
    keys: list[str] = []

    keys_raw = os.environ.get("DEEPGRAM_API_KEYS", "").strip()
    if keys_raw:
        for token in re.split(r"[,;\s]+", keys_raw):
            candidate = token.strip()
            if candidate:
                keys.append(candidate)

    for env_name in ("DEEPGRAM_API_KEY", "DEEPGRAM_API_KEY_FALLBACK"):
        candidate = os.environ.get(env_name, "").strip()
        if candidate:
            keys.append(candidate)

    deduped: list[str] = []
    seen: set[str] = set()
    for item in keys:
        if item in seen:
            continue
        seen.add(item)
        deduped.append(item)

    if deduped:
        return deduped

    raise ServiceError(
        stage="deepgram_config",
        code="missing_deepgram_api_key",
        message="set DEEPGRAM_API_KEY or DEEPGRAM_API_KEYS",
        status_code=500,
    )


def response_to_dict(value: Any) -> dict[str, Any]:
    if isinstance(value, dict):
        return value

    for method_name in ("to_dict", "model_dump"):
        method = getattr(value, method_name, None)
        if callable(method):
            result = method()
            if isinstance(result, dict):
                return result

    to_json = getattr(value, "to_json", None)
    if callable(to_json):
        try:
            raw = to_json()
            parsed = json.loads(raw)
            if isinstance(parsed, dict):
                return parsed
        except Exception:
            pass

    raise ServiceError(
        stage="deepgram_transcribe",
        code="invalid_deepgram_response",
        message="deepgram sdk returned an unexpected response shape",
        status_code=500,
    )


def is_timeout_message(message: str) -> bool:
    lowered = (message or "").strip().lower()
    if not lowered:
        return False
    timeout_tokens = (
        "timed out",
        "timeout",
        "write operation timed out",
        "read operation timed out",
        "connect timeout",
    )
    return any(token in lowered for token in timeout_tokens)


def parse_status_code(value: Any) -> int | None:
    try:
        if value is None:
            return None
        return int(value)
    except (TypeError, ValueError):
        return None


def is_retryable_api_error(status_code: int | None, message: str) -> bool:
    if status_code in {408, 409, 425, 429}:
        return True
    if isinstance(status_code, int) and status_code >= 500:
        return True
    return is_timeout_message(message)


def is_retryable_transport_error(exc: Exception) -> bool:
    if isinstance(exc, TimeoutError):
        return True
    class_name = exc.__class__.__name__.lower()
    if "timeout" in class_name:
        return True
    message = str(exc)
    if is_timeout_message(message):
        return True
    lowered = message.strip().lower()
    transient_tokens = (
        "temporarily unavailable",
        "connection reset",
        "connection aborted",
        "broken pipe",
        "server disconnected",
        "remote protocol error",
        "network is unreachable",
        "connection refused",
    )
    return any(token in lowered for token in transient_tokens)


def retry_delay_seconds(attempt: int, *, base_seconds: float, max_seconds: float) -> float:
    if attempt <= 1:
        return min(base_seconds, max_seconds)
    return min(max_seconds, base_seconds * (2 ** (attempt - 1)))


def deepgram_transcribe_file(
    *,
    client: Any,
    audio_bytes: bytes,
    options: dict[str, Any],
    request_options: dict[str, Any] | None,
) -> Any:
    if request_options:
        try:
            return client.listen.v1.media.transcribe_file(
                request=audio_bytes,
                request_options=request_options,
                **options,
            )
        except TypeError as exc:
            if "request_options" not in str(exc):
                raise
            logger.warning("deepgram sdk does not accept request_options; retrying without it")
    return client.listen.v1.media.transcribe_file(
        request=audio_bytes,
        **options,
    )


def speaker_to_label(raw_speaker: Any) -> str:
    if raw_speaker is None:
        return "SPEAKER_UNKNOWN"

    try:
        idx = int(raw_speaker)
        if idx < 0:
            return "SPEAKER_UNKNOWN"
        return f"SPEAKER_{idx:02d}"
    except (TypeError, ValueError):
        cleaned = re.sub(r"[^A-Za-z0-9]+", "_", str(raw_speaker)).strip("_")
        if not cleaned:
            return "SPEAKER_UNKNOWN"
        return f"SPEAKER_{cleaned.upper()}"


def build_speaker_roles(segments: list[dict[str, Any]]) -> dict[str, str]:
    totals: dict[str, float] = {}
    for item in segments:
        label = str(item.get("speaker_label") or "SPEAKER_UNKNOWN")
        start = float(item.get("start_seconds") or 0.0)
        end = float(item.get("end_seconds") or start)
        duration = max(0.0, end - start)
        totals[label] = totals.get(label, 0.0) + duration

    ordered = sorted(totals.items(), key=lambda pair: (-pair[1], pair[0]))
    mapping: dict[str, str] = {}
    for idx, (speaker_label, _) in enumerate(ordered):
        if idx == 0:
            mapping[speaker_label] = "role_primary"
        else:
            mapping[speaker_label] = f"role_secondary_{idx}"

    if not mapping:
        mapping["SPEAKER_UNKNOWN"] = "role_primary"

    return mapping


def format_seconds(seconds: float) -> str:
    value = max(0.0, float(seconds))
    hours = int(value // 3600)
    minutes = int((value % 3600) // 60)
    secs = value - (hours * 3600) - (minutes * 60)
    return f"{hours:02d}:{minutes:02d}:{secs:06.3f}"


def build_rag_text(
    *,
    source_id: str,
    video_id: str | None,
    language: str | None,
    speaker_roles: dict[str, str],
    segments: list[dict[str, Any]],
) -> str:
    lines: list[str] = [
        "<<<RAG_DOCUMENT>>>",
        "source: youtube_video",
        f"source_id: {source_id}",
    ]
    if video_id:
        lines.append(f"video_id: {video_id}")
    if language:
        lines.append(f"language: {language}")
    lines.extend(["provider: deepgram", "---"])

    for item in segments:
        speaker_label = str(item["speaker_label"])
        role_label = speaker_roles.get(speaker_label, "role_secondary")
        start = float(item["start_seconds"])
        end = float(item["end_seconds"])
        text = str(item["text_raw"]).strip()
        if not text:
            continue
        lines.append(
            f"[{format_seconds(start)} - {format_seconds(end)}] "
            f"[{role_label} | {speaker_label}] {text}"
        )

    lines.append("<<<END_RAG_DOCUMENT>>>")
    return "\n".join(lines).strip()


def extract_segments(transcript_data: dict[str, Any]) -> list[dict[str, Any]]:
    results = transcript_data.get("results") or {}
    utterances = results.get("utterances") or []

    out: list[dict[str, Any]] = []
    for idx, utterance in enumerate(utterances):
        text_raw = " ".join(str(utterance.get("transcript") or "").split()).strip()
        if not text_raw:
            continue

        start_seconds = float(utterance.get("start") or 0.0)
        end_seconds = float(utterance.get("end") or start_seconds)
        if end_seconds < start_seconds:
            end_seconds = start_seconds

        confidence_raw = utterance.get("confidence")
        confidence: float | None = None
        try:
            if confidence_raw is not None:
                confidence = float(confidence_raw)
        except (TypeError, ValueError):
            confidence = None

        out.append(
            {
                "segment_index": len(out),
                "start_seconds": round(start_seconds, 3),
                "end_seconds": round(end_seconds, 3),
                "speaker_label": speaker_to_label(utterance.get("speaker")),
                "text_raw": text_raw,
                "confidence": confidence,
            }
        )

    if out:
        return out

    channels = results.get("channels") or []
    alternatives: list[dict[str, Any]] = []
    if channels and isinstance(channels[0], dict):
        alternatives = channels[0].get("alternatives") or []

    transcript = ""
    if alternatives and isinstance(alternatives[0], dict):
        transcript = " ".join(str(alternatives[0].get("transcript") or "").split()).strip()

    if transcript:
        return [
            {
                "segment_index": 0,
                "start_seconds": 0.0,
                "end_seconds": 0.0,
                "speaker_label": "SPEAKER_UNKNOWN",
                "text_raw": transcript,
                "confidence": None,
            }
        ]

    raise ServiceError(
        stage="deepgram_parse",
        code="empty_transcript",
        message="deepgram did not return transcript text",
        status_code=422,
    )


def resolve_tool_path(tool: str) -> str:
    direct = shutil.which(tool)
    if direct:
        return direct
    return ""


def ensure_tool_exists(tool: str, stage: str, install_hint: str) -> str:
    tool_path = resolve_tool_path(tool)
    if tool_path:
        return tool_path
    raise ServiceError(
        stage=stage,
        code=f"{tool}_missing",
        message=f"{tool} is not installed",
        status_code=500,
        details={"install_hint": install_hint},
    )


def run_command(cmd: list[str], *, stage: str, cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    try:
        result = subprocess.run(
            cmd,
            cwd=str(cwd) if cwd else None,
            capture_output=True,
            text=True,
            check=False,
        )
    except OSError as exc:
        raise ServiceError(
            stage=stage,
            code="command_failed",
            message=str(exc),
            status_code=500,
            details={"command": cmd},
        ) from exc

    if result.returncode == 0:
        return result

    stderr = (result.stderr or "").strip()
    stdout = (result.stdout or "").strip()
    message = stderr if stderr else stdout
    if not message:
        message = f"command failed with exit code {result.returncode}"

    code = f"{stage}_failure"
    status_code = 500
    if stage == "download_audio":
        code, message, status_code = classify_yt_dlp_failure(stderr=stderr, stdout=stdout, fallback=message)

    raise ServiceError(
        stage=stage,
        code=code,
        message=message,
        status_code=status_code,
        details={"command": cmd, "stdout": stdout, "stderr": stderr},
    )


def classify_yt_dlp_failure(*, stderr: str, stdout: str, fallback: str) -> tuple[str, str, int]:
    stderr_lines = [line.strip() for line in (stderr or "").splitlines() if line.strip()]
    stdout_lines = [line.strip() for line in (stdout or "").splitlines() if line.strip()]

    error_lines = [line for line in (*stderr_lines, *stdout_lines) if line.upper().startswith("ERROR:")]
    message = error_lines[-1] if error_lines else (stderr_lines[-1] if stderr_lines else fallback)
    lowered = message.lower()
    stderr_lowered = (stderr or "").lower()

    code = "yt_dlp_failure"
    status_code = 500

    if "video unavailable" in lowered or "this video is unavailable" in lowered:
        code = "youtube_video_unavailable"
        status_code = 422
    elif "private video" in lowered:
        code = "youtube_video_private"
        status_code = 422
    elif "age-restricted" in lowered or "sign in to confirm your age" in lowered:
        code = "youtube_video_age_restricted"
        status_code = 422
    elif "no supported javascript runtime could be found" in stderr_lowered and not error_lines:
        code = "yt_dlp_js_runtime_missing"
        message = "no supported JavaScript runtime for yt-dlp; install node or set YTDLP_JS_RUNTIME"
        status_code = 500

    return code, message, status_code


def parse_youtube_video_id(raw_url: str) -> str:
    value = raw_url.strip()
    if not value:
        raise ServiceError(stage="validate_url", code="invalid_youtube_url", message="url is required", status_code=400)

    if "://" not in value:
        value = f"https://{value}"

    parsed = urlparse(value)
    host = parsed.netloc.lower().removeprefix("www.")
    video_id = ""

    if host in {"youtube.com", "m.youtube.com", "music.youtube.com"}:
        path_parts = [part for part in parsed.path.split("/") if part]
        if path_parts:
            if path_parts[0] == "watch":
                video_id = parse_qs(parsed.query).get("v", [""])[0]
            elif path_parts[0] in {"shorts", "embed", "live", "v"} and len(path_parts) > 1:
                video_id = path_parts[1]
        if not video_id:
            video_id = parse_qs(parsed.query).get("v", [""])[0]
    elif host == "youtu.be":
        path_parts = [part for part in parsed.path.split("/") if part]
        if path_parts:
            video_id = path_parts[0]
    else:
        raise ServiceError(
            stage="validate_url",
            code="invalid_youtube_url",
            message=f"unsupported host '{parsed.netloc}'",
            status_code=400,
        )

    video_id = video_id.strip().split("?", 1)[0].split("&", 1)[0].split("#", 1)[0].split("/", 1)[0]
    if not YOUTUBE_ID_RE.match(video_id):
        raise ServiceError(
            stage="validate_url",
            code="invalid_youtube_url",
            message="youtube url does not contain a valid video id",
            status_code=400,
        )

    return video_id


def yt_dlp_common_args(yt_dlp_bin: str) -> list[str]:
    args = [
        yt_dlp_bin,
        "--no-playlist",
        "--force-ipv4",
        "--retries",
        "3",
        "--fragment-retries",
        "3",
    ]

    js_runtime = os.environ.get("YTDLP_JS_RUNTIME", "").strip().lower()
    if js_runtime:
        if is_js_runtime_available(js_runtime):
            args.extend(["--no-js-runtimes", "--js-runtimes", js_runtime])
        else:
            logger.warning("YTDLP_JS_RUNTIME=%s is not available, skipping explicit js runtime", js_runtime)
    elif shutil.which("node"):
        args.extend(["--no-js-runtimes", "--js-runtimes", "node"])

    cookies_from_browser = os.environ.get("YTDLP_COOKIES_FROM_BROWSER", "").strip()
    cookies_file = os.environ.get("YTDLP_COOKIES_FILE", "").strip()
    use_cookies = bool(cookies_from_browser or cookies_file)

    player_client = os.environ.get("YTDLP_PLAYER_CLIENT", "android").strip()
    if player_client and not use_cookies:
        args.extend(["--extractor-args", f"youtube:player_client={player_client}"])

    if cookies_from_browser:
        args.extend(["--cookies-from-browser", cookies_from_browser])
    elif cookies_file:
        cookie_path = Path(cookies_file).expanduser()
        if not cookie_path.is_absolute():
            cookie_path = (Path.cwd() / cookie_path).resolve()
        if not cookie_path.exists():
            raise ServiceError(
                stage="download_audio",
                code="cookies_file_not_found",
                message=f"YTDLP_COOKIES_FILE does not exist: {cookie_path}",
                status_code=500,
            )
        args.extend(["--cookies", str(cookie_path)])

    return args


def is_js_runtime_available(js_runtime: str) -> bool:
    value = (js_runtime or "").strip()
    if not value:
        return False

    runtime_name = value
    runtime_path = ""
    if ":" in value:
        runtime_name, runtime_path = value.split(":", 1)
        runtime_name = runtime_name.strip()
        runtime_path = runtime_path.strip()

    if runtime_path:
        candidate = Path(runtime_path).expanduser()
        if not candidate.is_absolute():
            candidate = (Path.cwd() / candidate).resolve()
        return candidate.exists()

    if runtime_name == "":
        return False
    return shutil.which(runtime_name) is not None


def strip_yt_dlp_cookie_args(cmd: list[str]) -> list[str]:
    out: list[str] = []
    idx = 0
    while idx < len(cmd):
        token = cmd[idx]
        if token in {"--cookies-from-browser", "--cookies"}:
            idx += 2
            continue
        out.append(token)
        idx += 1
    return out


def run_yt_dlp_command(cmd: list[str], *, stage: str) -> subprocess.CompletedProcess[str]:
    try:
        return run_command(cmd, stage=stage)
    except ServiceError as exc:
        cookies_from_browser = os.environ.get("YTDLP_COOKIES_FROM_BROWSER", "").strip()
        if not cookies_from_browser or "--cookies-from-browser" not in cmd:
            raise
        message = (exc.message or "").lower()
        if "cookies database" not in message and "cookies-from-browser" not in message:
            raise
        fallback_cmd = strip_yt_dlp_cookie_args(cmd)
        if fallback_cmd == cmd:
            raise
        logger.warning("yt-dlp cookies-from-browser unavailable, retrying without cookies")
        return run_command(fallback_cmd, stage=stage)


def resolve_audio_storage_dir() -> Path:
    configured = os.environ.get("YOUTUBE_AUDIO_STORAGE_DIR", "/tmp/youtube_audio_storage").strip()
    base = Path(configured).expanduser()
    if not base.is_absolute():
        base = (Path.cwd() / base).resolve()
    base.mkdir(parents=True, exist_ok=True)
    return base.resolve()


def ensure_path_inside_dir(path: Path, root: Path, *, stage: str, code: str, message: str) -> Path:
    resolved_root = root.resolve()
    resolved_path = path.resolve()
    try:
        resolved_path.relative_to(resolved_root)
    except ValueError as exc:
        raise ServiceError(stage=stage, code=code, message=message, status_code=400) from exc
    return resolved_path


def safe_source_filename(value: str) -> str:
    out = re.sub(r"[^A-Za-z0-9_.-]+", "_", (value or "").strip())
    out = out.strip("._-")
    return out or "source"


def store_downloaded_audio(downloaded_audio: Path, source_id: str, video_id: str) -> Path:
    storage_dir = resolve_audio_storage_dir()
    source_part = safe_source_filename(source_id)
    suffix = downloaded_audio.suffix.lower() or ".wav"
    candidate = storage_dir / f"{source_part}_{video_id}{suffix}"
    index = 1
    while candidate.exists():
        candidate = storage_dir / f"{source_part}_{video_id}_{index}{suffix}"
        index += 1
    shutil.move(str(downloaded_audio), str(candidate))
    return ensure_path_inside_dir(
        candidate,
        storage_dir,
        stage="download_audio",
        code="invalid_audio_storage_path",
        message="stored audio path is outside storage directory",
    )


def resolve_stored_audio_path(raw_path: str) -> Path:
    value = (raw_path or "").strip()
    if not value:
        raise ServiceError(
            stage="download_audio",
            code="missing_audio_file_path",
            message="audio_file_path is required",
            status_code=400,
        )

    storage_dir = resolve_audio_storage_dir()
    candidate = Path(value).expanduser()
    if not candidate.is_absolute():
        candidate = storage_dir / candidate

    resolved = ensure_path_inside_dir(
        candidate,
        storage_dir,
        stage="download_audio",
        code="audio_file_path_not_allowed",
        message="audio_file_path is outside configured storage directory",
    )

    if not resolved.exists() or not resolved.is_file():
        raise ServiceError(
            stage="download_audio",
            code="audio_file_not_found",
            message=f"audio file not found: {resolved}",
            status_code=404,
        )
    return resolved


def guess_media_type(path: Path) -> str:
    guessed, _ = mimetypes.guess_type(path.name)
    if guessed:
        return guessed
    suffix = path.suffix.lower()
    if suffix == ".wav":
        return "audio/wav"
    if suffix == ".mp3":
        return "audio/mpeg"
    if suffix == ".m4a":
        return "audio/mp4"
    if suffix == ".opus":
        return "audio/ogg"
    if suffix == ".webm":
        return "audio/webm"
    return "application/octet-stream"


def probe_audio_duration(audio_path: Path) -> float | None:
    ffprobe_bin = resolve_tool_path("ffprobe")
    if not ffprobe_bin:
        return None

    try:
        result = run_command(
            [
                ffprobe_bin,
                "-v",
                "error",
                "-show_entries",
                "format=duration",
                "-of",
                "default=noprint_wrappers=1:nokey=1",
                str(audio_path),
            ],
            stage="download_audio",
        )
    except ServiceError:
        return None

    try:
        value = float((result.stdout or "").strip())
    except ValueError:
        return None
    if value <= 0:
        return None
    return round(value, 3)


def execute_download_audio(payload: DownloadAudioRequest) -> DownloadAudioResponse:
    source_id = payload.source_id.strip()
    if not SOURCE_ID_RE.match(source_id):
        raise ServiceError(
            stage="validate_request",
            code="invalid_source_id",
            message="source_id must match [A-Za-z0-9_.:-]{6,128}",
            status_code=400,
        )

    video_id = parse_youtube_video_id(payload.url)

    yt_dlp_bin = ensure_tool_exists(
        "yt-dlp",
        "download_audio",
        "install yt-dlp in the service environment",
    )
    ensure_tool_exists("ffmpeg", "download_audio", "install ffmpeg in the service environment")

    started = time.perf_counter()
    audio_format = os.environ.get("YTDLP_AUDIO_FORMAT", "mp3").strip().lower() or "mp3"
    default_audio_quality = "0" if audio_format in {"wav", "flac"} else "5"
    audio_quality = os.environ.get("YTDLP_AUDIO_QUALITY", default_audio_quality).strip()

    with tempfile.TemporaryDirectory(prefix=f"yt_audio_{source_id}_") as tmp_dir:
        tmp_path = Path(tmp_dir)
        out_template = str(tmp_path / f"{video_id}.%(ext)s")

        cmd = [
            *yt_dlp_common_args(yt_dlp_bin),
            "-f",
            "251/bestaudio/best",
            "-x",
            "--audio-format",
            audio_format,
        ]
        if audio_quality:
            cmd.extend(["--audio-quality", audio_quality])
        cmd.extend(["--output", out_template, payload.url])

        result = run_yt_dlp_command(cmd, stage="download_audio")

        title: str | None = None
        for line in (result.stdout or "").splitlines():
            if line.startswith("[ExtractAudio]"):
                continue
            if line.startswith("[download]"):
                continue
            if line.strip():
                title = line.strip()
                break

        candidates = sorted(tmp_path.glob(f"{video_id}.*"))
        downloaded_audio: Path | None = None
        for candidate in candidates:
            if candidate.is_file() and candidate.suffix.lower() in {".wav", ".mp3", ".m4a", ".webm", ".opus"}:
                downloaded_audio = candidate
                break

        if downloaded_audio is None:
            raise ServiceError(
                stage="download_audio",
                code="downloaded_audio_not_found",
                message="yt-dlp finished but audio file was not found",
                status_code=500,
            )

        stored_audio = store_downloaded_audio(downloaded_audio, source_id, video_id)
        elapsed = round(time.perf_counter() - started, 3)
        duration_seconds = probe_audio_duration(stored_audio)

        metadata: dict[str, Any] = {
            "provider": "yt_dlp",
            "audio_format": audio_format,
            "audio_quality": audio_quality if audio_quality else None,
            "elapsed_seconds": elapsed,
            "size_bytes": stored_audio.stat().st_size,
            "storage_dir": str(resolve_audio_storage_dir()),
            "duration_seconds": duration_seconds,
            "video_title": title,
        }

        logger.info(
            "download_audio done source_id=%s video_id=%s path=%s size=%s total=%0.2fs",
            source_id,
            video_id,
            stored_audio,
            metadata["size_bytes"],
            elapsed,
        )

        return DownloadAudioResponse(
            source_id=source_id,
            video_id=video_id,
            title=title,
            audio_file_path=str(stored_audio),
            metadata=metadata,
        )


def execute_transcribe_audio(payload: TranscribeAudioRequest) -> TranscribeAudioResponse:
    source_id = payload.source_id.strip()
    if not SOURCE_ID_RE.match(source_id):
        raise ServiceError(
            stage="validate_request",
            code="invalid_source_id",
            message="source_id must match [A-Za-z0-9_.:-]{6,128}",
            status_code=400,
        )

    audio_path = resolve_stored_audio_path(payload.audio_file_path)
    api_keys = resolve_deepgram_api_keys()
    language = normalize_language(payload.language)

    model = os.environ.get("DEEPGRAM_MODEL", "nova-3").strip() or "nova-3"
    detect_language = parse_bool_env("DEEPGRAM_DETECT_LANGUAGE", True)
    smart_format = parse_bool_env("DEEPGRAM_SMART_FORMAT", True)
    punctuate = parse_bool_env("DEEPGRAM_PUNCTUATE", True)
    paragraphs = parse_bool_env("DEEPGRAM_PARAGRAPHS", False)
    client_timeout_seconds = parse_float_env("DEEPGRAM_CLIENT_TIMEOUT_SECONDS", 900.0, min_value=10.0)
    request_timeout_seconds = parse_float_env("DEEPGRAM_REQUEST_TIMEOUT_SECONDS", 7200.0, min_value=30.0)
    sdk_max_retries = parse_int_env("DEEPGRAM_SDK_MAX_RETRIES", 3, min_value=0, max_value=10)
    attempts_per_key = parse_int_env("DEEPGRAM_ATTEMPTS_PER_KEY", 3, min_value=1, max_value=10)
    retry_base_seconds = parse_float_env("DEEPGRAM_RETRY_BASE_SECONDS", 2.0, min_value=0.1)
    retry_max_seconds = parse_float_env("DEEPGRAM_RETRY_MAX_SECONDS", 30.0, min_value=0.2)

    try:
        from deepgram import DeepgramClient
        from deepgram.core.api_error import ApiError
    except Exception as exc:
        raise ServiceError(
            stage="deepgram_setup",
            code="deepgram_sdk_missing",
            message="deepgram-sdk is not installed in the service environment",
            status_code=500,
            details={"error": str(exc)},
        ) from exc

    options: dict[str, Any] = {
        "model": model,
        "smart_format": smart_format,
        "punctuate": punctuate,
        "diarize": True,
        "utterances": True,
        "paragraphs": paragraphs,
    }
    if language:
        options["language"] = language
    else:
        options["detect_language"] = detect_language

    request_options: dict[str, Any] = {
        "timeout_in_seconds": int(request_timeout_seconds),
        "max_retries": sdk_max_retries,
    }

    started = time.perf_counter()
    response_data: dict[str, Any] | None = None
    used_key_index = 0
    last_error: Exception | None = None
    audio_size_bytes = audio_path.stat().st_size
    last_error_details: dict[str, Any] = {}
    debug_context = {
        "audio_size_bytes": audio_size_bytes,
        "client_timeout_seconds": client_timeout_seconds,
        "request_timeout_seconds": request_timeout_seconds,
        "attempts_per_key": attempts_per_key,
        "sdk_max_retries": sdk_max_retries,
    }

    for idx, api_key in enumerate(api_keys):
        used_key_index = idx + 1
        try:
            client = DeepgramClient(api_key=api_key, timeout=client_timeout_seconds)
        except TypeError:
            client = DeepgramClient(api_key=api_key)
        for attempt in range(1, attempts_per_key + 1):
            try:
                with audio_path.open("rb") as audio_file:
                    raw_response = deepgram_transcribe_file(
                        client=client,
                        audio_bytes=audio_file.read(),
                        options=options,
                        request_options=request_options,
                    )
                response_data = response_to_dict(raw_response)
                break
            except ApiError as exc:
                status_code = parse_status_code(
                    getattr(exc, "status_code", None) or getattr(exc, "status", None)
                )
                message = str(exc)
                last_error = exc
                last_error_details = {
                    "status_code": status_code,
                    "key_index": idx + 1,
                    "attempt": attempt,
                }

                if status_code in {401, 403}:
                    if idx < len(api_keys) - 1:
                        logger.warning("deepgram key #%s auth failed, trying next key", idx + 1)
                        break
                    raise ServiceError(
                        stage="deepgram_transcribe",
                        code="deepgram_api_error",
                        message=message,
                        status_code=502,
                        details={**debug_context, **last_error_details},
                    ) from exc

                retryable = is_retryable_api_error(status_code, message)
                if retryable and attempt < attempts_per_key:
                    delay = retry_delay_seconds(
                        attempt,
                        base_seconds=retry_base_seconds,
                        max_seconds=retry_max_seconds,
                    )
                    logger.warning(
                        "deepgram api retry key #%s attempt %s/%s in %.1fs: status=%s error=%s",
                        idx + 1,
                        attempt,
                        attempts_per_key,
                        delay,
                        status_code,
                        message,
                    )
                    time.sleep(delay)
                    continue

                if retryable and idx < len(api_keys) - 1:
                    logger.warning("deepgram key #%s exhausted retries, trying next key", idx + 1)
                    break

                error_code = "deepgram_timeout" if is_timeout_message(message) else "deepgram_api_error"
                raise ServiceError(
                    stage="deepgram_transcribe",
                    code=error_code,
                    message=message,
                    status_code=502,
                    details={**debug_context, **last_error_details},
                ) from exc
            except Exception as exc:
                last_error = exc
                last_error_details = {
                    "key_index": idx + 1,
                    "attempt": attempt,
                }
                retryable = is_retryable_transport_error(exc)
                if retryable and attempt < attempts_per_key:
                    delay = retry_delay_seconds(
                        attempt,
                        base_seconds=retry_base_seconds,
                        max_seconds=retry_max_seconds,
                    )
                    logger.warning(
                        "deepgram transport retry key #%s attempt %s/%s in %.1fs: %s",
                        idx + 1,
                        attempt,
                        attempts_per_key,
                        delay,
                        exc,
                    )
                    time.sleep(delay)
                    continue

                if retryable and idx < len(api_keys) - 1:
                    logger.warning("deepgram key #%s transport failed, trying next key", idx + 1)
                    break

                error_code = "deepgram_timeout" if is_timeout_message(str(exc)) else "deepgram_request_failed"
                raise ServiceError(
                    stage="deepgram_transcribe",
                    code=error_code,
                    message=str(exc),
                    status_code=502,
                    details={**debug_context, **last_error_details},
                ) from exc

        if response_data is not None:
            break

    if response_data is None:
        message = str(last_error) if last_error is not None else "unknown deepgram failure"
        error_code = "deepgram_timeout" if is_timeout_message(message) else "deepgram_no_response"
        raise ServiceError(
            stage="deepgram_transcribe",
            code=error_code,
            message=message,
            status_code=502,
            details={
                **debug_context,
                **last_error_details,
            },
        )

    segments = extract_segments(response_data)
    speaker_roles = build_speaker_roles(segments)
    for item in segments:
        item["role_label"] = speaker_roles.get(str(item["speaker_label"]), "role_secondary")

    channels = (response_data.get("results") or {}).get("channels") or []
    alternatives = channels[0].get("alternatives") if channels and isinstance(channels[0], dict) else None
    first_alt = alternatives[0] if alternatives and isinstance(alternatives[0], dict) else {}

    detected_language = normalize_language(first_alt.get("detected_language") if isinstance(first_alt, dict) else None)
    final_language = language or detected_language

    full_text_raw = " ".join(str(first_alt.get("transcript") or "").split()).strip()
    if not full_text_raw:
        full_text_raw = "\n".join(item["text_raw"] for item in segments).strip()

    rag_text = build_rag_text(
        source_id=source_id,
        video_id=payload.video_id,
        language=final_language,
        speaker_roles=speaker_roles,
        segments=segments,
    )

    elapsed = round(time.perf_counter() - started, 3)
    speaker_count = len(set(item["speaker_label"] for item in segments))
    duration_seconds = probe_audio_duration(audio_path)

    metadata = {
        "provider": "deepgram",
        "model": model,
        "elapsed_seconds": elapsed,
        "duration_seconds": duration_seconds,
        "segment_count": len(segments),
        "speaker_count": speaker_count,
        "audio_size_bytes": audio_size_bytes,
        "deepgram_timeouts": {
            "client_timeout_seconds": client_timeout_seconds,
            "request_timeout_seconds": request_timeout_seconds,
            "attempts_per_key": attempts_per_key,
            "sdk_max_retries": sdk_max_retries,
        },
        "deepgram_key_index": used_key_index,
        "deepgram_metadata": response_data.get("metadata"),
    }

    logger.info(
        "transcribe_audio done source_id=%s video_id=%s segments=%s speakers=%s total=%0.2fs",
        source_id,
        payload.video_id,
        len(segments),
        speaker_count,
        elapsed,
    )

    segment_models = [TranscribeSegment(**item) for item in segments]
    return TranscribeAudioResponse(
        source_id=source_id,
        video_id=payload.video_id,
        provider="deepgram",
        model=model,
        language=final_language,
        full_text_raw=full_text_raw,
        full_text_rag=rag_text,
        speaker_roles=speaker_roles,
        segments=segment_models,
        metadata=metadata,
    )


@app.exception_handler(ServiceError)
async def service_error_handler(_, exc: ServiceError):
    logger.warning("service_error stage=%s code=%s message=%s", exc.stage, exc.code, exc.message)
    return JSONResponse(
        status_code=exc.status_code,
        content={
            "error": {
                "stage": exc.stage,
                "code": exc.code,
                "message": exc.message,
                "details": exc.details,
            }
        },
    )


@app.get("/health")
def health() -> dict[str, Any]:
    return {"ok": True}


@app.post("/api/download-youtube-audio", response_model=DownloadAudioResponse)
def download_youtube_audio(payload: DownloadAudioRequest) -> DownloadAudioResponse:
    return execute_download_audio(payload)


@app.post("/api/transcribe-youtube-audio", response_model=TranscribeAudioResponse)
def transcribe_youtube_audio(payload: TranscribeAudioRequest) -> TranscribeAudioResponse:
    return execute_transcribe_audio(payload)


@app.get("/api/download-youtube-audio-file")
def download_youtube_audio_file(path: str = Query(..., min_length=1)):
    audio_path = resolve_stored_audio_path(path)
    media_type = guess_media_type(audio_path)
    return FileResponse(path=audio_path, media_type=media_type, filename=audio_path.name)
