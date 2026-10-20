-- worker 每轮查找超过总时限仍处于执行中的检测（进程中断后遗留），只扫描执行中的记录。
CREATE INDEX probe_runs_running_idx ON probe_runs (started_at) WHERE status = 'running';
