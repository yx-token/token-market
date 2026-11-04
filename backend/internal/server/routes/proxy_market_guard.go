package routes

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	adminhandler "github.com/yx-token/token-market/internal/handler/admin"
	"github.com/yx-token/token-market/internal/pkg/response"
	"github.com/yx-token/token-market/internal/service"
	"github.com/yx-token/token-market/market/enginehost"
)

type proxyMutationLocker func(ctx context.Context) (check proxyMutationChecker, release func(), err error)
type proxyMutationChecker = enginehost.ProxyMutationCheck

func lockProxyMutation(c *gin.Context, lock proxyMutationLocker) (proxyMutationChecker, func(), bool) {
	check, release, err := lock(c.Request.Context())
	if err != nil {
		idleCatWriteErr(c, err)
		c.Abort()
		return nil, nil, false
	}
	return check, release, true
}

func checkProxyMutation(c *gin.Context, check proxyMutationChecker, ids []int64) bool {
	if check == nil {
		return true
	}
	if err := check(c.Request.Context(), ids); err != nil {
		idleCatWriteErr(c, err)
		c.Abort()
		return false
	}
	return true
}

// proxyMutationGuard holds the market allocation lock across state read,
// destructive-diff detection, optional binding check, and native write.
func proxyMutationGuard(get func(context.Context, int64) (*service.Proxy, error), lock proxyMutationLocker) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.Next()
			return
		}
		check, release, ok := lockProxyMutation(c, lock)
		if !ok {
			return
		}
		defer release()
		input, bindErr := decodeProxyUpdate(c)
		if bindErr != nil {
			c.Next()
			return
		}

		current, err := get(c.Request.Context(), id)
		if err != nil {
			response.ErrorFrom(c, err)
			c.Abort()
			return
		}
		if proxyUpdateIsDestructive(input, current) && !checkProxyMutation(c, check, []int64{id}) {
			return
		}
		c.Next()
	}
}

func decodeProxyUpdate(c *gin.Context) (adminhandler.UpdateProxyRequest, error) {
	var input adminhandler.UpdateProxyRequest
	err := bindProxyGuardJSON(c, &input)
	return input, err
}

func proxyUpdateIsDestructive(input adminhandler.UpdateProxyRequest, current *service.Proxy) bool {
	endpointChanged := input.Protocol != "" && input.Protocol != current.Protocol ||
		input.Host != "" && input.Host != current.Host || input.Port != 0 && input.Port != current.Port ||
		input.Username != nil && *input.Username != current.Username ||
		input.Password != nil && *input.Password != current.Password
	stateChanged := input.Status != "" && input.Status != current.Status ||
		input.ExpiresAt.Set && !sameUnixExpiry(input.ExpiresAt.Value, current.ExpiresAt)
	return endpointChanged || stateChanged
}

func sameUnixExpiry(value *int64, current *time.Time) bool {
	if value == nil {
		return current == nil
	}
	return current != nil && current.Unix() == *value
}

func guardProxyIDs(get func(context.Context, int64) (*service.Proxy, error), lock proxyMutationLocker) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid proxy ID")
			c.Abort()
			return
		}
		check, release, ok := lockProxyMutation(c, lock)
		if !ok {
			return
		}
		defer release()
		if _, err := get(c.Request.Context(), id); err != nil {
			idleCatWriteErr(c, err)
			c.Abort()
			return
		}
		if !checkProxyMutation(c, check, []int64{id}) {
			return
		}
		c.Next()
	}
}

func proxyBatchMutationGuard(lock proxyMutationLocker) gin.HandlerFunc {
	return func(c *gin.Context) {
		check, release, ok := lockProxyMutation(c, lock)
		if !ok {
			return
		}
		defer release()
		var input struct {
			IDs []int64 `json:"ids"`
		}
		if err := bindProxyGuardJSON(c, &input); err != nil {
			c.Next()
			return
		}
		if !checkProxyMutation(c, check, input.IDs) {
			return
		}
		c.Next()
	}
}

func guardProxyImport(lock proxyMutationLocker, list func(ctx context.Context) ([]service.Proxy, error)) gin.HandlerFunc {
	return proxyImportMutationGuard(lock, list)
}

// proxyImportMutationGuard reads current inventory and determines affected
// proxies while holding the same lock as market allocation and native writes.
func proxyImportMutationGuard(lock proxyMutationLocker, list func(ctx context.Context) ([]service.Proxy, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		check, release, ok := lockProxyMutation(c, lock)
		if !ok {
			return
		}
		defer release()
		var input struct {
			Data struct {
				Proxies []struct {
					ProxyKey  string `json:"proxy_key"`
					Protocol  string `json:"protocol"`
					Host      string `json:"host"`
					Port      int    `json:"port"`
					Username  string `json:"username"`
					Password  string `json:"password"`
					Status    string `json:"status"`
					ExpiresAt *int64 `json:"expires_at"`
				} `json:"proxies"`
			} `json:"data"`
		}
		if err := bindProxyGuardJSON(c, &input); err != nil {
			c.Next()
			return
		}

		existing, err := list(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			c.Abort()
			return
		}
		byKey := make(map[string]service.Proxy, len(existing))
		for _, p := range existing {
			byKey[importProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)] = p
		}
		var ids []int64
		for _, p := range input.Data.Proxies {
			key := p.ProxyKey
			if key == "" {
				key = importProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)
			}
			current, ok := byKey[key]
			if !ok || normalizeProxyStatusForGuard(p.Status) == "" || normalizeProxyStatusForGuard(p.Status) == strings.ToLower(current.Status) && sameUnixExpiry(p.ExpiresAt, current.ExpiresAt) {
				continue
			}
			ids = append(ids, current.ID)
		}
		if len(ids) > 0 && !checkProxyMutation(c, check, ids) {
			return
		}
		c.Next()
	}
}

func importProxyKey(protocol, host string, port int, username, password string) string {
	return strings.TrimSpace(protocol) + "|" + strings.TrimSpace(host) + "|" + strconv.Itoa(port) + "|" + strings.TrimSpace(username) + "|" + strings.TrimSpace(password)
}

func normalizeProxyStatusForGuard(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "active":
		return "active"
	case "inactive", "disabled", "expired":
		return "inactive"
	default:
		return ""
	}
}

func bindProxyGuardJSON(c *gin.Context, value any) error {
	err := c.ShouldBindBodyWith(value, binding.JSON)
	if raw, exists := c.Get(gin.BodyBytesKey); exists {
		if body, ok := raw.([]byte); ok {
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
		}
	}
	return err
}
