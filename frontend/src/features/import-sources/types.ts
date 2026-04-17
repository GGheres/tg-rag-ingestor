export type ImportMethod =
  | "telegram"
  | "youtube"
  | "audio-upload"
  | "local-folder"
  | "json"
  | "hh-resumes"
  | "hh-public-vacancies";

export type TelegramMode = "channel-document" | "regular-source" | "message-links";

export type ImportStatus = "pending" | "running" | "success" | "partial" | "error";

export type ImportResult = {
  id: string;
  method: ImportMethod;
  telegramMode?: TelegramMode;
  title: string;
  url?: string;
  path?: string;
  status: ImportStatus;
  itemCount?: number;
  itemLabel: string;
  startedAt: string;
  finishedAt?: string;
  summary?: string;
  error?: string;
  documentId?: string;
};

export type ImportMethodOption = {
  id: ImportMethod;
  label: string;
  icon: string;
  description: string;
};

export type TelegramModeOption = {
  id: TelegramMode;
  label: string;
  description: string;
};

export const IMPORT_METHODS: ImportMethodOption[] = [
  {
    id: "telegram",
    label: "Telegram",
    icon: "\u2708",
    description: "Import from Telegram channels and messages",
  },
  {
    id: "youtube",
    label: "YouTube",
    icon: "\u25B6",
    description: "Import from YouTube videos, channels, or playlists",
  },
  {
    id: "audio-upload",
    label: "Audio to Text",
    icon: "\uD83C\uDFA4",
    description: "Upload audio file and transcribe with speaker roles",
  },
  {
    id: "local-folder",
    label: "Local Folder",
    icon: "\uD83D\uDCC1",
    description: "Scan and import files from a local directory",
  },
  {
    id: "json",
    label: "Import JSON",
    icon: "{ }",
    description: "Import data from a JSON file",
  },
  {
    id: "hh-resumes",
    label: "HH Resumes",
    icon: "\uD83C\uDFAF",
    description: "Extract resumes from HeadHunter vacancies",
  },
  {
    id: "hh-public-vacancies",
    label: "HH Public",
    icon: "\uD83D\uDD0D",
    description: "Import public HeadHunter vacancies into RAG",
  },
];

export const TELEGRAM_MODES: TelegramModeOption[] = [
  {
    id: "channel-document",
    label: "Channel as Document",
    description: "Import entire channel as a single RAG document",
  },
  {
    id: "regular-source",
    label: "Channel Source",
    description: "Add channel as a syncable source with individual messages",
  },
  {
    id: "message-links",
    label: "By Message Links",
    description: "Import specific messages by their links",
  },
];

export const MOCK_RESULTS: ImportResult[] = [
  {
    id: "r1",
    method: "telegram",
    telegramMode: "regular-source",
    title: "frontend_dev_channel",
    url: "https://t.me/frontend_dev_channel",
    status: "success",
    itemCount: 342,
    itemLabel: "messages",
    startedAt: "2026-03-30T09:12:00Z",
    finishedAt: "2026-03-30T09:14:22Z",
    summary: "342 messages fetched, 298 documents created, 1,204 chunks",
  },
  {
    id: "r2",
    method: "youtube",
    title: "React Server Components Deep Dive",
    url: "https://youtube.com/watch?v=abc123",
    status: "success",
    itemCount: 1,
    itemLabel: "videos",
    startedAt: "2026-03-30T08:45:00Z",
    finishedAt: "2026-03-30T08:52:10Z",
    summary: "Audio downloaded, transcribed, 24 chunks created",
  },
  {
    id: "r3",
    method: "local-folder",
    title: "Project Documentation",
    path: "/Users/dev/docs/project-specs",
    status: "partial",
    itemCount: 47,
    itemLabel: "files",
    startedAt: "2026-03-30T08:30:00Z",
    finishedAt: "2026-03-30T08:31:15Z",
    summary: "47 files found, 42 processed, 5 skipped (unsupported format)",
  },
  {
    id: "r4",
    method: "json",
    title: "knowledge_base_export.json",
    status: "error",
    itemCount: 0,
    itemLabel: "records",
    startedAt: "2026-03-30T08:20:00Z",
    finishedAt: "2026-03-30T08:20:01Z",
    error: "Invalid JSON structure: missing required field 'documents'",
  },
  {
    id: "r5",
    method: "telegram",
    telegramMode: "message-links",
    title: "Curated ML Resources",
    status: "running",
    itemCount: 12,
    itemLabel: "messages",
    startedAt: "2026-03-30T09:18:00Z",
    summary: "Processing 12 message links...",
  },
];
