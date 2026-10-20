package httpapi

import (
	"net/http"
	"strconv"

	"github.com/yx-token/idlicat/market/internal/iam"
	"github.com/yx-token/idlicat/market/internal/platform/httpx"
)

func (s *Server) listOrgUnits(w http.ResponseWriter, r *http.Request) error {
	units, err := s.IAM.ListOrgUnits(r.Context())
	if err != nil {
		return err
	}
	return ok(w, units)
}

func (s *Server) createOrgUnit(w http.ResponseWriter, r *http.Request) error {
	var in iam.OrgUnitInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.IAM.CreateOrgUnit(r.Context(), in)
	if err != nil {
		return err
	}
	s.audit(r, "org.unit.create", "org:"+strconv.FormatInt(u.ID, 10), in)
	return created(w, u)
}

func (s *Server) updateOrgUnit(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in iam.OrgUnitInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.IAM.UpdateOrgUnit(r.Context(), id, in)
	if err != nil {
		return err
	}
	s.audit(r, "org.unit.update", "org:"+strconv.FormatInt(id, 10), in)
	return ok(w, u)
}

func (s *Server) deleteOrgUnit(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.IAM.DeleteOrgUnit(r.Context(), id); err != nil {
		return err
	}
	s.audit(r, "org.unit.delete", "org:"+strconv.FormatInt(id, 10), nil)
	return ok(w, map[string]any{"ok": true})
}

func (s *Server) listIAMResources(w http.ResponseWriter, r *http.Request) error {
	list, err := s.IAM.ListResources(r.Context())
	if err != nil {
		return err
	}
	return ok(w, list)
}

func (s *Server) updateIAMResource(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if id == "" {
		return httpx.BadRequest("INVALID_ID", "resource id required")
	}
	var in iam.ResourceInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	res, err := s.IAM.UpdateResource(r.Context(), id, in)
	if err != nil {
		return err
	}
	s.audit(r, "org.resource.update", "resource:"+id, in)
	return ok(w, res)
}

func (s *Server) listIAMRoles(w http.ResponseWriter, r *http.Request) error {
	list, err := s.IAM.ListRoles(r.Context())
	if err != nil {
		return err
	}
	return ok(w, list)
}

func (s *Server) createIAMRole(w http.ResponseWriter, r *http.Request) error {
	var in iam.RoleInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	role, err := s.IAM.CreateRole(r.Context(), in)
	if err != nil {
		return err
	}
	s.audit(r, "org.role.create", "role:"+strconv.FormatInt(role.ID, 10), in)
	return created(w, role)
}

func (s *Server) updateIAMRole(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in iam.RoleInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	role, err := s.IAM.UpdateRole(r.Context(), id, in)
	if err != nil {
		return err
	}
	s.audit(r, "org.role.update", "role:"+strconv.FormatInt(id, 10), in)
	return ok(w, role)
}

func (s *Server) deleteIAMRole(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.IAM.DeleteRole(r.Context(), id); err != nil {
		return err
	}
	s.audit(r, "org.role.delete", "role:"+strconv.FormatInt(id, 10), nil)
	return ok(w, map[string]any{"ok": true})
}

func (s *Server) getIAMRoleResources(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	ids, err := s.IAM.RoleResourceIDs(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"resource_ids": ids})
}

func (s *Server) setIAMRoleResources(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var body struct {
		ResourceIDs []string `json:"resource_ids"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	if err := s.IAM.SetRoleResources(r.Context(), id, body.ResourceIDs); err != nil {
		return err
	}
	s.audit(r, "org.role.resources", "role:"+strconv.FormatInt(id, 10), body)
	return ok(w, map[string]any{"ok": true})
}

func (s *Server) getIAMRoleMembers(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	members, err := s.IAM.RoleMembers(r.Context(), id)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.ID)
	}
	return ok(w, map[string]any{"user_ids": ids, "members": members})
}

func (s *Server) setIAMRoleMembers(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var body struct {
		UserIDs []int64 `json:"user_ids"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	if err := s.IAM.SetRoleMembers(r.Context(), id, body.UserIDs); err != nil {
		return err
	}
	s.audit(r, "org.role.members", "role:"+strconv.FormatInt(id, 10), body)
	return ok(w, map[string]any{"ok": true})
}

func (s *Server) listIAMStaff(w http.ResponseWriter, r *http.Request) error {
	list, err := s.IAM.ListStaff(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	return ok(w, list)
}

func (s *Server) searchIAMUsers(w http.ResponseWriter, r *http.Request) error {
	list, err := s.IAM.ListUsersForPick(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	return ok(w, list)
}

// createIAMStaff 超管新建员工账号（开户 + 挂组织角色）。邮箱已存在时请改用「转为员工」。
func (s *Server) createIAMStaff(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Email      string  `json:"email"`
		Password   string  `json:"password"`
		RoleIDs    []int64 `json:"role_ids"`
		OrgUnitIDs []int64 `json:"org_unit_ids"`
		PrimaryOrg *int64  `json:"primary_org_id"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	if len(body.RoleIDs) == 0 {
		return httpx.BadRequest("ROLE_REQUIRED", "staff must have at least one org role")
	}
	createdUser, err := s.Accounts.CreateUserByAdmin(r.Context(), body.Email, body.Password)
	if err != nil {
		return err
	}
	u, err := s.IAM.AssignStaff(r.Context(), createdUser.ID, iam.StaffAssignInput{
		RoleIDs:    body.RoleIDs,
		OrgUnitIDs: body.OrgUnitIDs,
		PrimaryOrg: body.PrimaryOrg,
	})
	if err != nil {
		return err
	}
	s.audit(r, "org.staff.create", "user:"+strconv.FormatInt(createdUser.ID, 10), map[string]any{
		"email": body.Email, "role_ids": body.RoleIDs, "org_unit_ids": body.OrgUnitIDs,
	})
	return created(w, u)
}

func (s *Server) assignIAMStaff(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in iam.StaffAssignInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.IAM.AssignStaff(r.Context(), id, in)
	if err != nil {
		return err
	}
	s.audit(r, "org.staff.assign", "user:"+strconv.FormatInt(id, 10), in)
	return ok(w, u)
}

func (s *Server) myConsoleAccess(w http.ResponseWriter, r *http.Request) error {
	u := httpx.MustUser(r)
	access, err := s.IAM.ConsoleAccess(r.Context(), u.UserID, u.IsAdmin)
	if err != nil {
		return err
	}
	return ok(w, access)
}
