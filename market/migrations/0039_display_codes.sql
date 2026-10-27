-- 0039 上游 / 检测运行展示编号：YYYYMMDD + 随机串，界面用编号、不展示裸 id。

ALTER TABLE upstreams ADD COLUMN code text;
ALTER TABLE probe_runs ADD COLUMN code text;

UPDATE upstreams
SET code = to_char(timezone('Asia/Shanghai', created_at), 'YYYYMMDD')
	|| lower(substr(md5(id::text || random()::text), 1, 8))
WHERE code IS NULL OR code = '';

UPDATE probe_runs
SET code = to_char(timezone('Asia/Shanghai', queued_at), 'YYYYMMDD')
	|| lower(substr(md5(id::text || random()::text), 1, 8))
WHERE code IS NULL OR code = '';

ALTER TABLE upstreams ALTER COLUMN code SET NOT NULL;
ALTER TABLE probe_runs ALTER COLUMN code SET NOT NULL;

CREATE UNIQUE INDEX upstreams_code_uidx ON upstreams (code);
CREATE UNIQUE INDEX probe_runs_code_uidx ON probe_runs (code);
