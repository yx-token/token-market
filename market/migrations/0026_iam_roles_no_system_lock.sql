-- 组织角色不再区分「系统/自定义」锁定；超管可自由改删全部角色。
-- is_admin 超管旁路仍在；C 端与组织角色仍是两套概念。
UPDATE iam_roles SET is_system = false WHERE is_system = true;
