package admin

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yx-token/token-market/internal/handler/dto"
	"github.com/yx-token/token-market/internal/pkg/response"
	"github.com/yx-token/token-market/internal/service"
)

type KimiOAuthHandler struct {
	kimiOAuthService *service.KimiOAuthService
	adminService     service.AdminService
}

func NewKimiOAuthHandler(oauthService *service.KimiOAuthService, adminService service.AdminService) *KimiOAuthHandler {
	return &KimiOAuthHandler{kimiOAuthService: oauthService, adminService: adminService}
}

func (h *KimiOAuthHandler) DeviceCode(c *gin.Context) {
	var req struct {
		ProxyID *int64 `json:"proxy_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req = struct {
			ProxyID *int64 `json:"proxy_id"`
		}{}
	}
	result, err := h.kimiOAuthService.DeviceCode(c.Request.Context(), req.ProxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func (h *KimiOAuthHandler) Poll(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id"`
		ProxyID   *int64 `json:"proxy_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.SessionID) == "" {
		response.BadRequest(c, "session_id is required")
		return
	}
	result, err := h.kimiOAuthService.Poll(c.Request.Context(), req.SessionID, req.ProxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func (h *KimiOAuthHandler) RefreshToken(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
		ProxyID      *int64 `json:"proxy_id"`
		DeviceID     string `json:"device_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.kimiOAuthService.RefreshToken(c.Request.Context(), req.RefreshToken, req.ProxyID, req.DeviceID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func (h *KimiOAuthHandler) RefreshAccountToken(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account.Platform != service.PlatformKimi {
		response.BadRequest(c, "Account platform does not match Kimi OAuth endpoint")
		return
	}
	if account.Type != service.AccountTypeOAuth {
		response.BadRequest(c, "Cannot refresh non-OAuth account credentials")
		return
	}
	updated, err := h.kimiOAuthService.RefreshAccount(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(updated))
}
