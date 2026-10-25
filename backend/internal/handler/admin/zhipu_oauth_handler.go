package admin

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yx-token/token-market/internal/handler/dto"
	infraerrors "github.com/yx-token/token-market/internal/pkg/errors"
	"github.com/yx-token/token-market/internal/pkg/response"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
	"github.com/yx-token/token-market/internal/service"
)

type ZhipuOAuthHandler struct {
	oauth  *service.ZhipuOAuthService
	tokens *service.ZhipuTokenProvider
	admin  service.AdminService
}

func NewZhipuOAuthHandler(oauth *service.ZhipuOAuthService, tokens *service.ZhipuTokenProvider, admin service.AdminService) *ZhipuOAuthHandler {
	return &ZhipuOAuthHandler{oauth: oauth, tokens: tokens, admin: admin}
}

func writeZhipuOAuthError(c *gin.Context, err error) {
	var upstream *zhipu.APIError
	if errors.As(err, &upstream) {
		err = infraerrors.New(502, "ZHIPU_UPSTREAM_ERROR", "GLM 授权服务请求失败，请重试或重新授权")
	}
	response.ErrorFrom(c, err)
}

func (h *ZhipuOAuthHandler) Init(c *gin.Context) {
	var req struct {
		Provider zhipu.Provider `json:"provider"`
		ProxyID  *int64         `json:"proxy_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "provider is required")
		return
	}
	result, err := h.oauth.Init(c.Request.Context(), req.Provider, req.ProxyID)
	if err != nil {
		writeZhipuOAuthError(c, err)
		return
	}
	response.Success(c, result)
}
func (h *ZhipuOAuthHandler) Poll(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id"`
		ProxyID   *int64 `json:"proxy_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.SessionID) == "" {
		response.BadRequest(c, "session_id is required")
		return
	}
	result, err := h.oauth.Poll(c.Request.Context(), req.SessionID, req.ProxyID)
	if err != nil {
		writeZhipuOAuthError(c, err)
		return
	}
	response.Success(c, result)
}
func (h *ZhipuOAuthHandler) ExchangeCode(c *gin.Context) {
	var req service.ZhipuExchangeCodeInput
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.SessionID) == "" {
		response.BadRequest(c, "session_id is required")
		return
	}
	result, err := h.oauth.ExchangeCode(c.Request.Context(), req)
	if errors.Is(err, zhipu.ErrSessionBusy) {
		response.Error(c, 409, "授权会话正在处理，请稍后重试")
		return
	}
	if errors.Is(err, zhipu.ErrSessionExpired) {
		response.BadRequest(c, "授权会话已过期，请重新授权")
		return
	}
	if err != nil {
		writeZhipuOAuthError(c, err)
		return
	}
	response.Success(c, result)
}
func (h *ZhipuOAuthHandler) RefreshAccountToken(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	a, err := h.admin.GetAccount(c.Request.Context(), id)
	if err != nil {
		writeZhipuOAuthError(c, err)
		return
	}
	if !a.IsZhipuOAuth() {
		response.BadRequest(c, "Account must be a Zhipu OAuth account")
		return
	}
	updated, err := h.tokens.Account(c.Request.Context(), a, true)
	if err != nil {
		writeZhipuOAuthError(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(updated))
}
