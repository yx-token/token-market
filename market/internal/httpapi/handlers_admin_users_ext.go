package httpapi

import (
	"fmt"
	"net/http"

	"github.com/yx-token/token-market/market/internal/account"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func (s *Server) adminListUsers(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	rows, err := s.Accounts.AdminListUsers(r.Context(), account.AdminUserFilter{
		Q:         r.URL.Query().Get("q"),
		Status:    r.URL.Query().Get("status"),
		KYCStatus: r.URL.Query().Get("kyc_status"),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		return err
	}
	return ok(w, rows)
}

func (s *Server) adminGetUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	u, err := s.Accounts.AdminGetUser(r.Context(), id)
	if err != nil {
		return err
	}
	return ok(w, u)
}

func (s *Server) adminCreateUser(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Accounts.CreateUserByAdmin(r.Context(), in.Email, in.Password)
	if err != nil {
		return err
	}
	s.audit(r, "user.admin_create", fmt.Sprintf("user:%d", u.ID), map[string]string{"email": u.Email})
	detail, err := s.Accounts.AdminGetUser(r.Context(), u.ID)
	if err != nil {
		return ok(w, u)
	}
	return created(w, detail)
}

func (s *Server) adminUpdateUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in account.AdminUpdateUserInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := s.Accounts.AdminUpdateUser(r.Context(), adminID(r), id, in)
	if err != nil {
		return err
	}
	auditBody := map[string]any{
		"email": in.Email, "status": in.Status, "is_admin": in.IsAdmin,
		"kyc_status": in.KYCStatus, "password_set": in.Password != nil && *in.Password != "",
	}
	s.audit(r, "user.admin_update", fmt.Sprintf("user:%d", id), auditBody)
	return ok(w, u)
}
