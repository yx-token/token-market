-- The forward ID cursor is not a commit watermark. Persist a cyclic recovery
-- scan separately so late low-ID commits survive worker restarts.
INSERT INTO ingest_cursors(source) VALUES ('usage_logs_replay'), ('usage_logs_replay_end');
