create table if not exists telegram_message_links (
    id uuid primary key default gen_random_uuid(),
    source_id uuid not null references sources(id) on delete cascade,
    link_order integer not null,
    original_url text not null,
    canonical_url text not null,
    telegram_channel_id bigint,
    username text,
    telegram_message_id bigint not null,
    created_at timestamptz not null default now(),
    unique (source_id, link_order),
    unique (source_id, canonical_url)
);

create index if not exists idx_telegram_message_links_source_id
    on telegram_message_links(source_id);

create index if not exists idx_telegram_message_links_channel_message
    on telegram_message_links(telegram_channel_id, telegram_message_id);
