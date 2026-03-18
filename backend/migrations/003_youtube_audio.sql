alter table if exists sources add column if not exists provider text;
alter table if exists sources add column if not exists external_id text;

create index if not exists idx_sources_provider on sources(provider);
create index if not exists idx_sources_source_type_external_id on sources(source_type, external_id);

alter table if exists documents alter column raw_message_id drop not null;

create table if not exists youtube_audio_artifacts (
    id uuid primary key default gen_random_uuid(),
    source_id uuid not null references sources(id) on delete cascade,
    provider text not null,
    video_id text not null,
    audio_file_path text,
    audio_status text not null default 'queued',
    raw_json jsonb not null default '{}'::jsonb,
    error_text text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique(source_id)
);

create index if not exists idx_youtube_audio_artifacts_source_id on youtube_audio_artifacts(source_id);
create index if not exists idx_youtube_audio_artifacts_provider_video on youtube_audio_artifacts(provider, video_id);
