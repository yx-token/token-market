-- 状态对账：本系统管理的账号的分组成员
SELECT ag.account_id, ag.group_id
FROM account_groups ag
JOIN accounts a ON a.id = ag.account_id
WHERE a.name LIKE 'tm-%' AND a.deleted_at IS NULL
ORDER BY ag.account_id, ag.group_id
