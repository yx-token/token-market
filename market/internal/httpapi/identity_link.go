package httpapi

import (
	"context"
	"log/slog"

	"github.com/yx-token/idlicat/market/internal/account"
)

// attachEngineIdentity 让市场用户与同邮箱的引擎用户对齐。管理员只核对引擎账号；买家/卖家再绑定影子并登录换令牌。
// 引擎失败不回滚市场登录，调用方把错误放进响应。
func (s *Server) attachEngineIdentity(ctx context.Context, u *account.User, password string) (int64, bool, error) {
	if u == nil || s.Funds == nil || s.Funds.Shadow == nil {
		return 0, false, nil
	}
	shadow := s.Funds.Shadow
	if u.IsAdmin {
		if shadow.Replica == nil {
			return 0, false, nil
		}
		engineUser, err := shadow.Replica.UserByEmail(ctx, u.Email)
		if err != nil || engineUser == nil {
			return 0, false, err
		}
		return engineUser.ID, false, nil
	}
	engineID, err := shadow.EnsureSameEmailUser(ctx, u.Email, password)
	if err != nil {
		return 0, false, err
	}
	it, err := shadow.BindLinkedBuyer(ctx, u.ID, engineID, u.Email, password)
	if err != nil {
		return engineID, false, err
	}
	return engineID, it != nil && it.Status == "active", nil
}

func (s *Server) noteEngineLink(ctx context.Context, payload map[string]any, u *account.User, password string) {
	id, ready, err := s.attachEngineIdentity(ctx, u, password)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("engine identity link failed", "email", u.Email, "err", err)
		} else {
			slog.Warn("engine identity link failed", "email", u.Email, "err", err)
		}
		payload["engine_link_error"] = err.Error()
	}
	if id > 0 {
		payload["engine_user_id"] = id
		payload["engine_token_ready"] = ready
	}
}
