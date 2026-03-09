create extension if not exists "pgcrypto";

alter table if exists sources add column if not exists telegram_channel_id bigint;
alter table if exists sources add column if not exists telegram_access_hash bigint;
alter table if exists sources add column if not exists last_error text;
alter table if exists sources add column if not exists created_at timestamptz not null default now();
alter table if exists sources add column if not exists updated_at timestamptz not null default now();

create table if not exists raw_messages (
    id uuid primary key default gen_random_uuid(),
    source_id uuid not null references sources(id) on delete cascade,
    telegram_message_id bigint not null,
    grouped_id bigint,
    posted_at timestamptz,
    edited_at timestamptz,
    text_raw text,
    caption_raw text,
    raw_json jsonb not null default '{}'::jsonb,
    media_json jsonb,
    entities_json jsonb,
    reactions_json jsonb,
    views_count integer,
    forwards_count integer,
    reply_to_message_id bigint,
    forward_from_name text,
    forward_from_chat text,
    has_media boolean not null default false,
    fetch_job_id uuid,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique(source_id, telegram_message_id)
);

alter table if exists documents add column if not exists raw_message_id uuid references raw_messages(id) on delete cascade;
alter table if exists documents add column if not exists title text;
alter table if exists documents add column if not exists text_original text;
alter table if exists documents add column if not exists summary text;
alter table if exists documents add column if not exists content_hash text;
alter table if exists documents add column if not exists simhash text;
alter table if exists documents add column if not exists dedupe_group text;
alter table if exists documents add column if not exists duplicate_of uuid references documents(id);
alter table if exists documents add column if not exists is_forward boolean not null default false;
alter table if exists documents add column if not exists forward_source text;
alter table if exists documents add column if not exists tags jsonb not null default '[]'::jsonb;
alter table if exists documents add column if not exists links jsonb not null default '[]'::jsonb;
alter table if exists documents add column if not exists indexed_at timestamptz;
alter table if exists documents alter column metadata set default '{}'::jsonb;

update documents
set content_hash = md5(coalesce(text_clean, ''))
where content_hash is null;

alter table if exists chunks add column if not exists token_count int;
alter table if exists chunks add column if not exists char_count int;
alter table if exists chunks add column if not exists embedding_model text;
alter table if exists chunks add column if not exists embedding_error text;
alter table if exists chunks add column if not exists qdrant_point_id text;

create table if not exists jobs (
    id uuid primary key default gen_random_uuid(),
    job_type text not null,
    source_id uuid references sources(id) on delete set null,
    status text not null,
    payload jsonb not null default '{}'::jsonb,
    progress_total int not null default 0,
    progress_done int not null default 0,
    result_json jsonb,
    error_text text,
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz not null default now()
);

create table if not exists exports (
    id uuid primary key default gen_random_uuid(),
    export_type text not null,
    status text not null,
    source_id uuid references sources(id) on delete set null,
    file_path text,
    row_count int not null default 0,
    error_text text,
    created_at timestamptz not null default now(),
    finished_at timestamptz
);

create index if not exists idx_sources_username on sources(username);
create index if not exists idx_sources_status on sources(status);
create index if not exists idx_raw_messages_source_id on raw_messages(source_id);
create index if not exists idx_raw_messages_posted_at on raw_messages(posted_at);
create index if not exists idx_documents_source_id on documents(source_id);
create index if not exists idx_documents_raw_message_id on documents(raw_message_id);
create index if not exists idx_documents_hash on documents(source_id, content_hash);
create index if not exists idx_documents_is_duplicate on documents(is_duplicate);
create index if not exists idx_chunks_document_id on chunks(document_id);
create index if not exists idx_jobs_source_id on jobs(source_id);
create index if not exists idx_jobs_status on jobs(status);
create index if not exists idx_exports_source_id on exports(source_id);
create index if not exists idx_exports_status on exports(status);
