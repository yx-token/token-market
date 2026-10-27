-- 0040 展示编号随机段改为纯数字（YYYYMMDD + 8 位数字）。

UPDATE upstreams
SET code = to_char(timezone('Asia/Shanghai', created_at), 'YYYYMMDD')
	|| lpad(((abs(('x' || substr(md5(id::text || 'u' || random()::text), 1, 8))::bit(32)::int) % 100000000))::text, 8, '0');

UPDATE probe_runs
SET code = to_char(timezone('Asia/Shanghai', queued_at), 'YYYYMMDD')
	|| lpad(((abs(('x' || substr(md5(id::text || 'r' || random()::text), 1, 8))::bit(32)::int) % 100000000))::text, 8, '0');
