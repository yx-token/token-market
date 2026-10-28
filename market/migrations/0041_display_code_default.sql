-- 0041 展示编号缺省值：集成测试与裸 INSERT 不写 code 时仍满足 NOT NULL。

ALTER TABLE upstreams ALTER COLUMN code SET DEFAULT (
	to_char(timezone('Asia/Shanghai', now()), 'YYYYMMDD')
	|| lpad((floor(random() * 100000000))::bigint::text, 8, '0')
);

ALTER TABLE probe_runs ALTER COLUMN code SET DEFAULT (
	to_char(timezone('Asia/Shanghai', now()), 'YYYYMMDD')
	|| lpad((floor(random() * 100000000))::bigint::text, 8, '0')
);
