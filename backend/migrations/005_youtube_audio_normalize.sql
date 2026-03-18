update sources
set provider = 'yt_dlp',
    updated_at = now()
where source_type = 'youtube_video'
  and coalesce(provider, '') <> 'yt_dlp';

update youtube_audio_artifacts
set provider = 'yt_dlp',
    updated_at = now()
where coalesce(provider, '') <> 'yt_dlp';

update youtube_audio_artifacts
set audio_status = case
        when audio_status in ('failed', 'error') then 'download_failed'
        when audio_status in ('done', 'succeeded', 'completed', 'success') then 'audio_downloaded'
        else audio_status
    end,
    updated_at = now()
where audio_status in ('failed', 'error', 'done', 'succeeded', 'completed', 'success');
