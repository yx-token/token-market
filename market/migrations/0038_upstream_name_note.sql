-- 0038 上游展示名称与运营备注：管理端代接入时可填写，列表/详情优先展示名称。

ALTER TABLE upstreams ADD COLUMN name text;
ALTER TABLE upstreams ADD COLUMN note text;
