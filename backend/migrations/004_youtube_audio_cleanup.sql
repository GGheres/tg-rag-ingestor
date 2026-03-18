-- Migration for environments that previously had legacy transcript tables.
-- Copies downloadable audio artifact data, then removes deprecated tables.

do $$
begin
    if to_regclass('public.transcript_artifacts') is not null then
        insert into youtube_audio_artifacts (
            source_id,
            provider,
            video_id,
            audio_file_path,
            audio_status,
            raw_json,
            error_text,
            created_at,
            updated_at
        )
        select
            source_id,
            provider,
            video_id,
            audio_file_path,
            coalesce(nullif(transcript_status, ''), 'queued') as audio_status,
            coalesce(raw_json, '{}'::jsonb),
            error_text,
            created_at,
            updated_at
        from transcript_artifacts
        on conflict (source_id) do update
        set provider = excluded.provider,
            video_id = excluded.video_id,
            audio_file_path = excluded.audio_file_path,
            audio_status = excluded.audio_status,
            raw_json = excluded.raw_json,
            error_text = excluded.error_text,
            updated_at = now();
    end if;
end $$;

drop table if exists transcript_segments;
drop table if exists speaker_role_mappings;
drop table if exists transcript_artifacts;

update sources
set provider = 'yt_dlp',
    updated_at = now()
where source_type = 'youtube_video'
  and coalesce(provider, '') <> 'yt_dlp';

update youtube_audio_artifacts
set provider = 'yt_dlp',
    updated_at = now()
where coalesce(provider, '') <> 'yt_dlp';
