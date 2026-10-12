// Package iam 组织权限：组织单元、菜单资源、角色与用户授权（RBAC）。
// users.is_admin 仍为超管旁路，可见全部资源。
package iam

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Service struct{ DB DB }

func New(db DB) *Service { return &Service{DB: db} }

type OrgUnit struct {
	ID        int64     `json:"id"`
	ParentID  *int64    `json:"parent_id,omitempty"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	Sort      int       `json:"sort"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Resource struct {
	ID        string    `json:"id"`
	ParentID  *string   `json:"parent_id,omitempty"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // dir | menu | action
	Path      string    `json:"path"`
	Icon      string    `json:"icon"`
	Sort      int       `json:"sort"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Role struct {
	ID          int64     `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system"`
	MemberCount int       `json:"member_count,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type StaffUser struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	IsAdmin   bool      `json:"is_admin"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	Roles     []Role    `json:"roles,omitempty"`
	OrgUnits  []OrgUnit `json:"org_units,omitempty"`
}

type ConsoleAccess struct {
	IsAdmin     bool     `json:"is_admin"`
	ResourceIDs []string `json:"resource_ids"`
	MenuPaths   []string `json:"menu_paths"`
}

func (s *Service) ListOrgUnits(ctx context.Context) ([]OrgUnit, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, parent_id, name, code, sort, enabled, created_at, updated_at FROM org_units ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgUnit
	for rows.Next() {
		var u OrgUnit
		if err := rows.Scan(&u.ID, &u.ParentID, &u.Name, &u.Code, &u.Sort, &u.Enabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

type OrgUnitInput struct {
	ParentID *int64  `json:"parent_id"`
	Name     string  `json:"name"`
	Code     string  `json:"code"`
	Sort     *int    `json:"sort"`
	Enabled  *bool   `json:"enabled"`
}

func (s *Service) CreateOrgUnit(ctx context.Context, in OrgUnitInput) (*OrgUnit, error) {
	name := strings.TrimSpace(in.Name)
	code := strings.TrimSpace(in.Code)
	if name == "" || code == "" {
		return nil, httpx.BadRequest("INVALID_INPUT", "name and code required")
	}
	sort := 0
	if in.Sort != nil {
		sort = *in.Sort
	}
	var id int64
	err := s.DB.QueryRow(ctx, `INSERT INTO org_units (parent_id, name, code, sort) VALUES ($1,$2,$3,$4) RETURNING id`,
		in.ParentID, name, code, sort).Scan(&id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, httpx.Conflict("ORG_CODE_EXISTS", "org code already exists")
	}
	if err != nil {
		return nil, err
	}
	return s.getOrgUnit(ctx, id)
}

func (s *Service) UpdateOrgUnit(ctx context.Context, id int64, in OrgUnitInput) (*OrgUnit, error) {
	u, err := s.getOrgUnit(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != "" {
		u.Name = strings.TrimSpace(in.Name)
	}
	if in.Code != "" {
		u.Code = strings.TrimSpace(in.Code)
	}
	if in.ParentID != nil {
		if *in.ParentID == id {
			return nil, httpx.BadRequest("INVALID_PARENT", "cannot be own parent")
		}
		u.ParentID = in.ParentID
	}
	if in.Sort != nil {
		u.Sort = *in.Sort
	}
	if in.Enabled != nil {
		u.Enabled = *in.Enabled
	}
	_, err = s.DB.Exec(ctx, `UPDATE org_units SET parent_id=$2, name=$3, code=$4, sort=$5, enabled=$6, updated_at=now() WHERE id=$1`,
		id, u.ParentID, u.Name, u.Code, u.Sort, u.Enabled)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, httpx.Conflict("ORG_CODE_EXISTS", "org code already exists")
	}
	if err != nil {
		return nil, err
	}
	return s.getOrgUnit(ctx, id)
}

func (s *Service) DeleteOrgUnit(ctx context.Context, id int64) error {
	var kids, members int
	if err := s.DB.QueryRow(ctx, `SELECT COUNT(*) FROM org_units WHERE parent_id=$1`, id).Scan(&kids); err != nil {
		return err
	}
	if kids > 0 {
		return httpx.BadRequest("ORG_HAS_CHILDREN", "请先删除或移走下级组织")
	}
	if err := s.DB.QueryRow(ctx, `SELECT COUNT(*) FROM iam_user_orgs WHERE org_unit_id=$1`, id).Scan(&members); err != nil {
		return err
	}
	if members > 0 {
		return httpx.BadRequest("ORG_HAS_MEMBERS", "请先解除该组织下的员工归属")
	}
	tag, err := s.DB.Exec(ctx, `DELETE FROM org_units WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("ORG_NOT_FOUND", "org unit not found")
	}
	return nil
}

func (s *Service) getOrgUnit(ctx context.Context, id int64) (*OrgUnit, error) {
	var u OrgUnit
	err := s.DB.QueryRow(ctx, `SELECT id, parent_id, name, code, sort, enabled, created_at, updated_at FROM org_units WHERE id=$1`, id).
		Scan(&u.ID, &u.ParentID, &u.Name, &u.Code, &u.Sort, &u.Enabled, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ORG_NOT_FOUND", "org unit not found")
	}
	return &u, err
}

func (s *Service) ListResources(ctx context.Context) ([]Resource, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, parent_id, name, kind, path, icon, sort, enabled, created_at, updated_at FROM iam_resources ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Resource
	for rows.Next() {
		var r Resource
		if err := rows.Scan(&r.ID, &r.ParentID, &r.Name, &r.Kind, &r.Path, &r.Icon, &r.Sort, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type ResourceInput struct {
	Name    *string `json:"name"`
	Sort    *int    `json:"sort"`
	Enabled *bool   `json:"enabled"`
	Icon    *string `json:"icon"`
	Path    *string `json:"path"`
}

func (s *Service) UpdateResource(ctx context.Context, id string, in ResourceInput) (*Resource, error) {
	r, err := s.getResource(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		r.Name = strings.TrimSpace(*in.Name)
	}
	if in.Sort != nil {
		r.Sort = *in.Sort
	}
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	if in.Icon != nil {
		r.Icon = *in.Icon
	}
	if in.Path != nil {
		r.Path = *in.Path
	}
	_, err = s.DB.Exec(ctx, `UPDATE iam_resources SET name=$2, sort=$3, enabled=$4, icon=$5, path=$6, updated_at=now() WHERE id=$1`,
		id, r.Name, r.Sort, r.Enabled, r.Icon, r.Path)
	if err != nil {
		return nil, err
	}
	return s.getResource(ctx, id)
}

func (s *Service) getResource(ctx context.Context, id string) (*Resource, error) {
	var r Resource
	err := s.DB.QueryRow(ctx, `SELECT id, parent_id, name, kind, path, icon, sort, enabled, created_at, updated_at FROM iam_resources WHERE id=$1`, id).
		Scan(&r.ID, &r.ParentID, &r.Name, &r.Kind, &r.Path, &r.Icon, &r.Sort, &r.Enabled, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("RESOURCE_NOT_FOUND", "resource not found")
	}
	return &r, err
}

func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT r.id, r.code, r.name, r.description, r.is_system, r.created_at, r.updated_at,
		       (SELECT COUNT(*) FROM iam_user_roles ur WHERE ur.role_id = r.id)::int
		FROM iam_roles r ORDER BY r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Code, &role.Name, &role.Description, &role.IsSystem, &role.CreatedAt, &role.UpdatedAt, &role.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

type RoleInput struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Service) CreateRole(ctx context.Context, in RoleInput) (*Role, error) {
	code := strings.TrimSpace(in.Code)
	name := strings.TrimSpace(in.Name)
	if code == "" || name == "" {
		return nil, httpx.BadRequest("INVALID_INPUT", "code and name required")
	}
	var id int64
	err := s.DB.QueryRow(ctx, `INSERT INTO iam_roles (code, name, description) VALUES ($1,$2,$3) RETURNING id`,
		code, name, strings.TrimSpace(in.Description)).Scan(&id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, httpx.Conflict("ROLE_EXISTS", "role code already exists")
	}
	if err != nil {
		return nil, err
	}
	return s.getRole(ctx, id)
}

func (s *Service) UpdateRole(ctx context.Context, id int64, in RoleInput) (*Role, error) {
	role, err := s.getRole(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != "" {
		role.Name = strings.TrimSpace(in.Name)
	}
	role.Description = strings.TrimSpace(in.Description)
	if in.Code != "" {
		role.Code = strings.TrimSpace(in.Code)
	}
	_, err = s.DB.Exec(ctx, `UPDATE iam_roles SET code=$2, name=$3, description=$4, updated_at=now() WHERE id=$1`,
		id, role.Code, role.Name, role.Description)
	if err != nil {
		return nil, err
	}
	return s.getRole(ctx, id)
}

func (s *Service) DeleteRole(ctx context.Context, id int64) error {
	if _, err := s.getRole(ctx, id); err != nil {
		return err
	}
	_, err := s.DB.Exec(ctx, `DELETE FROM iam_roles WHERE id=$1`, id)
	return err
}

func (s *Service) getRole(ctx context.Context, id int64) (*Role, error) {
	var role Role
	err := s.DB.QueryRow(ctx, `
		SELECT r.id, r.code, r.name, r.description, r.is_system, r.created_at, r.updated_at,
		       (SELECT COUNT(*) FROM iam_user_roles ur WHERE ur.role_id = r.id)::int
		FROM iam_roles r WHERE r.id=$1`, id).
		Scan(&role.ID, &role.Code, &role.Name, &role.Description, &role.IsSystem, &role.CreatedAt, &role.UpdatedAt, &role.MemberCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ROLE_NOT_FOUND", "role not found")
	}
	return &role, err
}

func (s *Service) RoleResourceIDs(ctx context.Context, roleID int64) ([]string, error) {
	if _, err := s.getRole(ctx, roleID); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT resource_id FROM iam_role_resources WHERE role_id=$1 ORDER BY resource_id`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Service) SetRoleResources(ctx context.Context, roleID int64, resourceIDs []string) error {
	if _, err := s.getRole(ctx, roleID); err != nil {
		return err
	}
	tx, ok := s.DB.(interface {
		Begin(ctx context.Context) (pgx.Tx, error)
	})
	_ = tx
	_ = ok
	// 无事务接口时用顺序执行（pgxpool 支持 Begin，但我们用 DB 接口；直接删插）
	if _, err := s.DB.Exec(ctx, `DELETE FROM iam_role_resources WHERE role_id=$1`, roleID); err != nil {
		return err
	}
	for _, rid := range resourceIDs {
		rid = strings.TrimSpace(rid)
		if rid == "" {
			continue
		}
		if _, err := s.DB.Exec(ctx, `INSERT INTO iam_role_resources (role_id, resource_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, roleID, rid); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) RoleMemberIDs(ctx context.Context, roleID int64) ([]int64, error) {
	members, err := s.RoleMembers(ctx, roleID)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(members))
	for _, m := range members {
		out = append(out, m.ID)
	}
	return out, nil
}

// RoleMembers 返回角色成员详情（邮箱等），供详情页列表展示。
func (s *Service) RoleMembers(ctx context.Context, roleID int64) ([]StaffUser, error) {
	if _, err := s.getRole(ctx, roleID); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `
		SELECT u.id, u.email, u.is_admin, u.status, u.created_at
		FROM users u
		JOIN iam_user_roles ur ON ur.user_id = u.id
		WHERE ur.role_id = $1
		ORDER BY u.id`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StaffUser
	for rows.Next() {
		var u StaffUser
		if err := rows.Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Status, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Service) SetRoleMembers(ctx context.Context, roleID int64, userIDs []int64) error {
	if _, err := s.getRole(ctx, roleID); err != nil {
		return err
	}
	if _, err := s.DB.Exec(ctx, `DELETE FROM iam_user_roles WHERE role_id=$1`, roleID); err != nil {
		return err
	}
	for _, uid := range userIDs {
		if _, err := s.DB.Exec(ctx, `INSERT INTO iam_user_roles (user_id, role_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, roleID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListStaff(ctx context.Context, q string) ([]StaffUser, error) {
	q = strings.TrimSpace(strings.ToLower(q))
	rows, err := s.DB.Query(ctx, `
		SELECT u.id, u.email, u.is_admin, u.status, u.created_at
		FROM users u
		WHERE ($1 = '' OR u.email LIKE '%' || $1 || '%' OR u.id::text = $1)
		  AND (
		    u.is_admin
		    OR EXISTS (SELECT 1 FROM iam_user_roles ur WHERE ur.user_id = u.id)
		    OR EXISTS (SELECT 1 FROM iam_user_orgs uo WHERE uo.user_id = u.id)
		  )
		ORDER BY u.id
		LIMIT 500`, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StaffUser
	for rows.Next() {
		var u StaffUser
		if err := rows.Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Status, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		roles, err := s.userRoles(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		orgs, err := s.userOrgs(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Roles = roles
		out[i].OrgUnits = orgs
	}
	return out, nil
}

func (s *Service) ListUsersForPick(ctx context.Context, q string) ([]StaffUser, error) {
	q = strings.TrimSpace(strings.ToLower(q))
	rows, err := s.DB.Query(ctx, `
		SELECT id, email, is_admin, status, created_at FROM users
		WHERE $1 = '' OR email LIKE '%' || $1 || '%' OR id::text = $1
		ORDER BY id LIMIT 100`, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StaffUser
	for rows.Next() {
		var u StaffUser
		if err := rows.Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Status, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

type StaffAssignInput struct {
	RoleIDs    []int64 `json:"role_ids"`
	OrgUnitIDs []int64 `json:"org_unit_ids"`
	PrimaryOrg *int64  `json:"primary_org_id"`
}

func (s *Service) AssignStaff(ctx context.Context, userID int64, in StaffAssignInput) (*StaffUser, error) {
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, userID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, httpx.NotFound("USER_NOT_FOUND", "user not found")
	}
	if _, err := s.DB.Exec(ctx, `DELETE FROM iam_user_roles WHERE user_id=$1`, userID); err != nil {
		return nil, err
	}
	for _, rid := range in.RoleIDs {
		if _, err := s.DB.Exec(ctx, `INSERT INTO iam_user_roles (user_id, role_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, userID, rid); err != nil {
			return nil, err
		}
	}
	if _, err := s.DB.Exec(ctx, `DELETE FROM iam_user_orgs WHERE user_id=$1`, userID); err != nil {
		return nil, err
	}
	primary := int64(0)
	if in.PrimaryOrg != nil {
		primary = *in.PrimaryOrg
	}
	for _, oid := range in.OrgUnitIDs {
		isPrimary := oid == primary || (primary == 0 && len(in.OrgUnitIDs) == 1 && oid == in.OrgUnitIDs[0])
		if _, err := s.DB.Exec(ctx, `INSERT INTO iam_user_orgs (user_id, org_unit_id, is_primary) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
			userID, oid, isPrimary); err != nil {
			return nil, err
		}
	}
	roles, err := s.userRoles(ctx, userID)
	if err != nil {
		return nil, err
	}
	orgs, err := s.userOrgs(ctx, userID)
	if err != nil {
		return nil, err
	}
	var u StaffUser
	err = s.DB.QueryRow(ctx, `SELECT id, email, is_admin, status, created_at FROM users WHERE id=$1`, userID).
		Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Roles = roles
	u.OrgUnits = orgs
	return &u, nil
}

func (s *Service) userRoles(ctx context.Context, userID int64) ([]Role, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT r.id, r.code, r.name, r.description, r.is_system, r.created_at, r.updated_at, 0
		FROM iam_roles r
		JOIN iam_user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1 ORDER BY r.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Code, &role.Name, &role.Description, &role.IsSystem, &role.CreatedAt, &role.UpdatedAt, &role.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

func (s *Service) userOrgs(ctx context.Context, userID int64) ([]OrgUnit, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT o.id, o.parent_id, o.name, o.code, o.sort, o.enabled, o.created_at, o.updated_at
		FROM org_units o
		JOIN iam_user_orgs uo ON uo.org_unit_id = o.id
		WHERE uo.user_id = $1 ORDER BY uo.is_primary DESC, o.sort, o.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgUnit
	for rows.Next() {
		var u OrgUnit
		if err := rows.Scan(&u.ID, &u.ParentID, &u.Name, &u.Code, &u.Sort, &u.Enabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ConsoleAccess 返回当前用户可见的控制台资源；is_admin 超管返回全部启用资源。
func (s *Service) ConsoleAccess(ctx context.Context, userID int64, isAdmin bool) (*ConsoleAccess, error) {
	out := &ConsoleAccess{IsAdmin: isAdmin, ResourceIDs: []string{}, MenuPaths: []string{}}
	var rows pgx.Rows
	var err error
	if isAdmin {
		rows, err = s.DB.Query(ctx, `SELECT id, path, kind FROM iam_resources WHERE enabled ORDER BY sort, id`)
	} else {
		rows, err = s.DB.Query(ctx, `
			SELECT DISTINCT r.id, r.path, r.kind
			FROM iam_resources r
			JOIN iam_role_resources rr ON rr.resource_id = r.id
			JOIN iam_user_roles ur ON ur.role_id = rr.role_id
			WHERE ur.user_id = $1 AND r.enabled
			ORDER BY r.id`, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seenPath := map[string]bool{}
	for rows.Next() {
		var id, path, kind string
		if err := rows.Scan(&id, &path, &kind); err != nil {
			return nil, err
		}
		out.ResourceIDs = append(out.ResourceIDs, id)
		if kind == "menu" && path != "" && !seenPath[path] {
			seenPath[path] = true
			out.MenuPaths = append(out.MenuPaths, path)
		}
	}
	return out, rows.Err()
}

// HasConsoleAccess 非超管需至少有一个角色资源，或仍兼容 is_admin。
func (s *Service) HasConsoleAccess(ctx context.Context, userID int64, isAdmin bool) (bool, error) {
	if isAdmin {
		return true, nil
	}
	var n int
	err := s.DB.QueryRow(ctx, `
		SELECT COUNT(*) FROM iam_user_roles ur
		JOIN iam_role_resources rr ON rr.role_id = ur.role_id
		JOIN iam_resources r ON r.id = rr.resource_id AND r.enabled
		WHERE ur.user_id = $1`, userID).Scan(&n)
	return n > 0, err
}

// HasAnyResource 超管恒真；否则需拥有任一 resourceID（精确命中角色勾选的资源码）。
func (s *Service) HasAnyResource(ctx context.Context, userID int64, isAdmin bool, resourceIDs ...string) (bool, error) {
	if isAdmin {
		return true, nil
	}
	ids := make([]string, 0, len(resourceIDs))
	for _, id := range resourceIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return s.HasConsoleAccess(ctx, userID, false)
	}
	var n int
	err := s.DB.QueryRow(ctx, `
		SELECT COUNT(*) FROM iam_user_roles ur
		JOIN iam_role_resources rr ON rr.role_id = ur.role_id
		JOIN iam_resources r ON r.id = rr.resource_id AND r.enabled
		WHERE ur.user_id = $1 AND rr.resource_id = ANY($2)`, userID, ids).Scan(&n)
	return n > 0, err
}
