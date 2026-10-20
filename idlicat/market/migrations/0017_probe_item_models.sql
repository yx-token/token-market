-- 0017 题目可限定适用模型；每次检测改为测全部适用题目，不再抽题。

ALTER TABLE probe_items ADD COLUMN models text[] NOT NULL DEFAULT '{}';   -- 空表示全部模型

-- detection_policies.items_per_run 不再使用；保留该列，便于站内更新失败时回退到旧版本程序。
