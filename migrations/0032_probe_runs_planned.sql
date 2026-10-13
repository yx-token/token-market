-- 检测记录列表要显示计划总题数；从执行快照现算需要逐行解析可达数 MB 的快照，改为写快照时同时记录。
-- planned 为快照中所有实例的步骤数之和，与 probe_results 每步一行的口径一致；没有快照的旧记录保持 NULL。
ALTER TABLE probe_runs ADD COLUMN planned integer;

UPDATE probe_runs SET planned = (
    SELECT SUM(jsonb_array_length(instance->'steps'))::int
    FROM jsonb_array_elements(execution_snapshot->'instances') AS instance
    WHERE jsonb_typeof(instance->'steps') = 'array'
)
WHERE jsonb_typeof(execution_snapshot->'instances') = 'array';
