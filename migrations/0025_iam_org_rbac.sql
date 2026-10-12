-- 0025 组织权限（RBAC）：组织单元、菜单资源、角色、用户授权。
-- 参考 cc-base：用户─角色─菜单；组织单元用于部门归属；is_admin 仍为超管旁路。

CREATE TABLE org_units (
    id          bigserial PRIMARY KEY,
    parent_id   bigint REFERENCES org_units (id) ON DELETE RESTRICT,
    name        text        NOT NULL,
    code        text        NOT NULL UNIQUE,
    sort        int         NOT NULL DEFAULT 0,
    enabled     boolean     NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX org_units_parent_idx ON org_units (parent_id, sort, id);

CREATE TABLE iam_resources (
    id          text PRIMARY KEY,
    parent_id   text REFERENCES iam_resources (id) ON DELETE RESTRICT,
    name        text        NOT NULL,
    kind        text        NOT NULL CHECK (kind IN ('dir', 'menu', 'action')),
    path        text        NOT NULL DEFAULT '',
    icon        text        NOT NULL DEFAULT '',
    sort        int         NOT NULL DEFAULT 0,
    enabled     boolean     NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (id <> ''),
    CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX iam_resources_parent_idx ON iam_resources (parent_id, sort, id);

CREATE TABLE iam_roles (
    id          bigserial PRIMARY KEY,
    code        text        NOT NULL UNIQUE,
    name        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    is_system   boolean     NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE iam_role_resources (
    role_id     bigint NOT NULL REFERENCES iam_roles (id) ON DELETE CASCADE,
    resource_id text   NOT NULL REFERENCES iam_resources (id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, resource_id)
);

CREATE TABLE iam_user_roles (
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id bigint NOT NULL REFERENCES iam_roles (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);
CREATE INDEX iam_user_roles_role_idx ON iam_user_roles (role_id);

CREATE TABLE iam_user_orgs (
    user_id     bigint  NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    org_unit_id bigint  NOT NULL REFERENCES org_units (id) ON DELETE CASCADE,
    is_primary  boolean NOT NULL DEFAULT false,
    PRIMARY KEY (user_id, org_unit_id)
);
CREATE INDEX iam_user_orgs_org_idx ON iam_user_orgs (org_unit_id);

COMMENT ON TABLE org_units IS '组织单元（部门树）';
COMMENT ON TABLE iam_resources IS '控制台菜单/目录/功能资源';
COMMENT ON TABLE iam_roles IS '角色；勾选资源后挂到用户';
COMMENT ON TABLE iam_role_resources IS '角色 ↔ 资源';
COMMENT ON TABLE iam_user_roles IS '用户 ↔ 角色';
COMMENT ON TABLE iam_user_orgs IS '用户 ↔ 组织单元';

-- 默认组织
INSERT INTO org_units (id, parent_id, name, code, sort) VALUES
  (1, NULL, '平台总部', 'root', 0),
  (2, 1, '运营中心', 'ops', 10),
  (3, 1, '风控合规', 'risk', 20),
  (4, 1, '技术支持', 'support', 30)
ON CONFLICT (code) DO NOTHING;
SELECT setval(pg_get_serial_sequence('org_units', 'id'), (SELECT COALESCE(MAX(id), 1) FROM org_units));

-- 控制台菜单资源（与 ConsoleLayout 对齐）
INSERT INTO iam_resources (id, parent_id, name, kind, path, icon, sort) VALUES
  ('console', NULL, '控制台', 'dir', '', 'layout-dashboard', 0),

  ('console.duty', 'console', '值班', 'dir', '', 'activity', 10),
  ('console.duty.overview', 'console.duty', '概览', 'menu', '/console', 'layout-dashboard', 10),
  ('console.duty.funds', 'console.duty', '充值 / 退款 / 提现', 'menu', '/console/funds', 'wallet', 20),
  ('console.duty.quality', 'console.duty', '质量与申诉', 'menu', '/console/quality', 'badge-check', 30),
  ('console.duty.risk', 'console.duty', '风控', 'menu', '/console/risk', 'shield-alert', 40),
  ('console.duty.billing', 'console.duty', '计费异常', 'menu', '/console/billing', 'circle-alert', 50),

  ('console.supply', 'console', '闲置TOKEN', 'dir', '', 'server', 20),
  ('console.supply.upstreams', 'console.supply', '上游', 'menu', '/console/upstreams', 'server', 10),
  ('console.supply.pools', 'console.supply', '默认分组', 'menu', '/console/pools', 'layers', 20),
  ('console.supply.vendors', 'console.supply', '厂商', 'menu', '/console/vendors', 'building-2', 30),
  ('console.supply.proxies', 'console.supply', '代理', 'menu', '/console/proxies', 'globe', 40),

  ('console.detect', 'console', '检测', 'dir', '', 'flask-conical', 30),
  ('console.detect.detection', 'console.detect', '检测策略', 'menu', '/console/detection', 'flask-conical', 10),
  ('console.detect.inspection', 'console.detect', '质检项', 'menu', '/console/inspection-checks', 'badge-check', 20),
  ('console.detect.probe_records', 'console.detect', '检测记录', 'menu', '/console/probe-records', 'activity', 30),
  ('console.detect.third_party', 'console.detect', '第三方检测', 'menu', '/console/third-party', 'scan-search', 40),
  ('console.detect.probe_items', 'console.detect', '题库与基线', 'menu', '/console/probe-items', 'book-open', 50),

  ('console.ops', 'console', '运营', 'dir', '', 'layout-dashboard', 40),
  ('console.ops.rec_factors', 'console.ops', '推荐因子', 'menu', '/console/ops/recommend/factors', 'sliders-horizontal', 10),
  ('console.ops.rec_profiles', 'console.ops', '推荐类型', 'menu', '/console/ops/recommend/profiles', 'layers', 20),
  ('console.ops.rec_tiles', 'console.ops', '首页四格', 'menu', '/console/ops/recommend/tiles', 'layout-dashboard', 30),
  ('console.ops.categories', 'console.ops', '市场分类', 'menu', '/console/ops/categories', 'filter', 40),
  ('console.ops.hot', 'console.ops', '热搜词', 'menu', '/console/ops/hot-searches', 'search', 50),
  ('console.ops.extras', 'console.ops', '其他配置', 'menu', '/console/ops/extras', 'settings', 60),

  ('console.govern', 'console', '治理', 'dir', '', 'settings', 50),
  ('console.govern.users', 'console.govern', '用户与实名', 'menu', '/console/users', 'users', 10),
  ('console.govern.payments', 'console.govern', '支付配置', 'menu', '/console/payments', 'credit-card', 20),
  ('console.govern.settings', 'console.govern', '系统设置', 'menu', '/console/settings', 'settings', 30),
  ('console.govern.system_update', 'console.govern', '系统更新', 'menu', '/console/system-update', 'refresh-cw', 40),
  ('console.govern.audit', 'console.govern', '审计日志', 'menu', '/console/audit', 'scroll-text', 50),

  ('console.org', 'console', '组织管理', 'dir', '', 'network', 60),
  ('console.org.units', 'console.org', '组织架构', 'menu', '/console/org/units', 'building-2', 10),
  ('console.org.roles', 'console.org', '角色管理', 'menu', '/console/org/roles', 'shield', 20),
  ('console.org.menus', 'console.org', '菜单管理', 'menu', '/console/org/menus', 'menu', 30),
  ('console.org.staff', 'console.org', '员工授权', 'menu', '/console/org/staff', 'user-cog', 40)
ON CONFLICT (id) DO NOTHING;

-- 系统角色
INSERT INTO iam_roles (id, code, name, description, is_system) VALUES
  (1, 'platform_admin', '平台管理员', '全部控制台菜单与组织管理', true),
  (2, 'operator', '运营', '值班 + 闲置TOKEN + 运营配置', true),
  (3, 'risk_officer', '风控', '风控、质量、审计、用户实名', true),
  (4, 'viewer', '只读观察', '概览与审计只读入口（后续可细到 action）', true)
ON CONFLICT (code) DO NOTHING;
SELECT setval(pg_get_serial_sequence('iam_roles', 'id'), (SELECT COALESCE(MAX(id), 1) FROM iam_roles));

-- 平台管理员：全部资源
INSERT INTO iam_role_resources (role_id, resource_id)
SELECT 1, id FROM iam_resources
ON CONFLICT DO NOTHING;

-- 运营角色
INSERT INTO iam_role_resources (role_id, resource_id)
SELECT 2, id FROM iam_resources
WHERE id LIKE 'console.duty%'
   OR id LIKE 'console.supply%'
   OR id LIKE 'console.ops%'
   OR id IN ('console', 'console.duty', 'console.supply', 'console.ops')
ON CONFLICT DO NOTHING;

-- 风控角色
INSERT INTO iam_role_resources (role_id, resource_id)
SELECT 3, id FROM iam_resources
WHERE id IN (
  'console', 'console.duty', 'console.duty.overview', 'console.duty.quality', 'console.duty.risk',
  'console.govern', 'console.govern.users', 'console.govern.audit',
  'console.detect', 'console.detect.detection', 'console.detect.inspection',
  'console.detect.probe_records', 'console.detect.third_party', 'console.detect.probe_items'
)
ON CONFLICT DO NOTHING;

-- 只读
INSERT INTO iam_role_resources (role_id, resource_id)
SELECT 4, id FROM iam_resources
WHERE id IN ('console', 'console.duty', 'console.duty.overview', 'console.govern', 'console.govern.audit')
ON CONFLICT DO NOTHING;

-- 现有 is_admin 用户自动挂平台管理员 + 总部
INSERT INTO iam_user_roles (user_id, role_id)
SELECT u.id, 1 FROM users u WHERE u.is_admin
ON CONFLICT DO NOTHING;

INSERT INTO iam_user_orgs (user_id, org_unit_id, is_primary)
SELECT u.id, 1, true FROM users u WHERE u.is_admin
ON CONFLICT DO NOTHING;
