DO $$
DECLARE
    sql text;
BEGIN
    SELECT 'TRUNCATE TABLE ' || string_agg(format('%I.%I', schemaname, tablename), ', ')
           || ' RESTART IDENTITY CASCADE'
      INTO sql
      FROM pg_tables
     WHERE schemaname = 'public'
       AND tablename IN (
            'telegram_message_links',
            'youtube_audio_artifacts',
            'chunks',
            'documents',
            'raw_messages',
            'jobs',
            'exports',
            'sources'
       );

    IF sql IS NOT NULL THEN
        EXECUTE sql;
    END IF;
END $$;
