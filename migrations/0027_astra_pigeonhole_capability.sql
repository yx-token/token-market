-- 将未改题面的 gpt-6-astra 糖果题原地移到能力质检，保留 ID 与历史结果。
-- 严格识别旧内置定义；多行匹配或稳定键已占用时不猜测归属，留给管理员核对。
-- 未匹配的管理员题保留，启动同步按稳定键补入新的标准内置题。
WITH candidates AS (
    SELECT id FROM probe_items
    WHERE suite = 'reasoning'
      AND category = 'pigeonhole'
      AND 'gpt-6-astra' = ANY(models)
      AND (builtin_key IS NULL OR builtin_key = '')
      AND check_key = ''
      AND prompt = $astra_candy$在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）

| | 苹果味 | 桃子味 | 西瓜味 |
|---|---|---|---|
| 圆形 | 7 | 9 | 8 |
| 五角星 | 7 | 6 | 4 |$astra_candy$
), singleton AS (
    SELECT min(id) AS id FROM candidates HAVING count(*) = 1
)
UPDATE probe_items AS item
SET suite = 'inspection_capability',
    check_key = 'D07',
    builtin_key = 'inspection/pigeonhole/astra/1',
    modes = ARRAY['full', 'quick']::text[],
    version = item.version + 1,
    updated_at = now()
FROM singleton
WHERE item.id = singleton.id
  AND NOT EXISTS (
      SELECT 1 FROM probe_items WHERE builtin_key = 'inspection/pigeonhole/astra/1'
  );
