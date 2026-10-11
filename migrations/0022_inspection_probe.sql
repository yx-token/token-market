-- 质检协议与执行底座：只增列、放宽题目套件，不改变旧题或检测策略。
ALTER TABLE probe_items
    ADD COLUMN check_key text NOT NULL DEFAULT '',
    ADD COLUMN request jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN generator jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN modes text[] NOT NULL DEFAULT '{}',
    ADD COLUMN builtin_key text UNIQUE,
    ADD COLUMN weight double precision NOT NULL DEFAULT 1 CHECK (weight >= 0);
ALTER TABLE probe_items DROP CONSTRAINT probe_items_suite_check;
ALTER TABLE probe_items ADD CONSTRAINT probe_items_suite_check CHECK
    (suite IN ('iq', 'reasoning', 'long_context', 'tool_call', 'inspection_authenticity',
              'inspection_capability', 'inspection_function', 'inspection_output', 'inspection_performance'));
ALTER TABLE probe_results
    ADD COLUMN check_key text NOT NULL DEFAULT '',
    ADD COLUMN instance_idx integer NOT NULL DEFAULT 0,
    ADD COLUMN step integer NOT NULL DEFAULT 0,
    ADD COLUMN instance_hash text NOT NULL DEFAULT '',
    ADD COLUMN generator_version text NOT NULL DEFAULT '',
    ADD COLUMN input_tokens bigint CHECK (input_tokens >= 0),
    ADD COLUMN cache_read_tokens bigint CHECK (cache_read_tokens >= 0),
    ADD COLUMN cache_creation_tokens bigint CHECK (cache_creation_tokens >= 0),
    ADD COLUMN stop_reason text NOT NULL DEFAULT '',
    ADD COLUMN effort text NOT NULL DEFAULT '',
    ADD COLUMN flags text[] NOT NULL DEFAULT '{}',
    ADD COLUMN step_state jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN error_body text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX probe_results_instance_idx ON probe_results (run_id, item_id, instance_idx, step)
    WHERE instance_hash <> '';
ALTER TABLE probe_runs
    ADD COLUMN mode text NOT NULL DEFAULT 'legacy',
    ADD COLUMN execution_snapshot jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN deadline_at timestamptz;
ALTER TABLE detection_policies
    ADD COLUMN item_parallelism integer NOT NULL DEFAULT 4 CHECK (item_parallelism BETWEEN 1 AND 8),
    ADD COLUMN full_triggers text[] NOT NULL DEFAULT '{onboarding,appeal,admin}',
    ADD COLUMN token_budgets jsonb NOT NULL DEFAULT '{}';
ALTER TABLE quality_verdicts
    ADD COLUMN quality_factor double precision,
    ADD COLUMN checks jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN gates jsonb NOT NULL DEFAULT '[]';
ALTER TABLE upstreams
    ADD COLUMN quality_factor double precision NOT NULL DEFAULT 1,
    ADD COLUMN appraised_at timestamptz;
ALTER TABLE upstream_quality_state
    ADD COLUMN pending_grade text,
    ADD COLUMN pending_grade_count integer NOT NULL DEFAULT 0,
    ADD COLUMN last_failed_checks text[] NOT NULL DEFAULT '{}';
ALTER TABLE vendor_configs
    ADD COLUMN appraisal_base jsonb NOT NULL DEFAULT '{}';
