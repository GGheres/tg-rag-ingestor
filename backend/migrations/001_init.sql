create extension if not exists "pgcrypto";

create table if not exists sources (
    id uuid primary key default gen_random_uuid(),
    source_type text not null,
    telegram_channel_id bigint,
    telegram_access_hash bigint,
    username text,
    title text,
    url text not null unique,
    status text not null default 'active',
    last_message_id bigint,
    last_synced_at timestamptz,
    last_error text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

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

create table if not exists documents (
    id uuid primary key default gen_random_uuid(),
    source_id uuid not null references sources(id) on delete cascade,
    raw_message_id uuid not null references raw_messages(id) on delete cascade,
    external_doc_id text not null unique,
    title text,
    text_clean text not null,
    text_original text,
    summary text,
    language_code text,
    content_hash text not null,
    simhash text,
    dedupe_group text,
    quality_score double precision,
    is_duplicate boolean not null default false,
    duplicate_of uuid references documents(id),
    is_forward boolean not null default false,
    forward_source text,
    tags jsonb not null default '[]'::jsonb,
    links jsonb not null default '[]'::jsonb,
    metadata jsonb not null default '{}'::jsonb,
    published_at timestamptz,
    indexed_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table if not exists chunks (
    id uuid primary key default gen_random_uuid(),
    document_id uuid not null references documents(id) on delete cascade,
    chunk_index int not null,
    text text not null,
    token_count int,
    char_count int,
    metadata jsonb not null default '{}'::jsonb,
    embedding_status text not null default 'pending',
    embedding_model text,
    embedding_error text,
    qdrant_point_id text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique(document_id, chunk_index)
);

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
