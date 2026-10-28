package listing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

type AuthorizationPoll struct {
	Status   string    `json:"status"`
	Interval int       `json:"interval"`
	Upstream *Upstream `json:"upstream,omitempty"`
}

func (s *Service) lockSellerOAuth(ctx context.Context, sellerID, upstreamID int64) (*Upstream, func(), error) {
	if _, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID); err != nil {
		return nil, nil, err
	}
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return nil, nil, err
	}
	upstream, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		release()
		return nil, nil, err
	}
	return upstream, release, nil
}

func (s *Service) PollAuthorization(ctx context.Context, sellerID, upstreamID int64) (*AuthorizationPoll, error) {
	upstream, release, err := s.lockSellerOAuth(ctx, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	defer release()
	spec, ok := oauthSpecs[upstream.Platform][upstream.AuthType]
	if !ok || spec.flowKind() == oauthLinkCode {
		return nil, httpx.BadRequest("AUTH_POLL_NOT_SUPPORTED", "this authorization type does not support polling")
	}
	if (upstream.Sub2APIAccountID == nil && upstream.Status != StatusAuthorizing) || upstream.Status == StatusRemoving || upstream.Status == StatusRemoved {
		return nil, ErrBadState
	}
	var sessionID, kind, authType, lastStatus string
	var expires time.Time
	var nextPollAt *time.Time
	var interval int
	err = s.DB.QueryRow(ctx, `SELECT session_id, kind, auth_type, expires_at, next_poll_at,
		COALESCE(interval_seconds, 5), COALESCE(last_status, 'pending') FROM upstream_oauth_sessions WHERE upstream_id = $1`, upstream.ID).
		Scan(&sessionID, &kind, &authType, &expires, &nextPollAt, &interval, &lastStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return &AuthorizationPoll{Status: "expired"}, nil
	}
	if err != nil {
		return nil, err
	}
	if !s.now().Before(expires) || kind != spec.flowKind() || authType != upstream.AuthType {
		return s.endOAuth(ctx, upstream.ID, "expired")
	}
	interval = max(1, interval)
	if nextPollAt != nil && s.now().Before(*nextPollAt) {
		if lastStatus != "slow_down" {
			lastStatus = "pending"
		}
		return &AuthorizationPoll{Status: lastStatus, Interval: interval}, nil
	}
	proxyID, err := s.proxySub2APIID(ctx, upstream)
	if err != nil {
		return nil, err
	}
	if _, err := s.DB.Exec(ctx, `UPDATE upstream_oauth_sessions SET next_poll_at = $2 WHERE upstream_id = $1`,
		upstream.ID, s.now().Add(time.Duration(interval)*time.Second)); err != nil {
		return nil, err
	}
	result, err := pollOAuth(ctx, s.Admin, spec, sessionID, proxyID)
	if err != nil {
		return nil, httpx.Unavailable("AUTH_POLL_FAILED", "authorization service unavailable, please retry")
	}
	status := str(result, "status")
	switch status {
	case "pending", "slow_down":
		returnedInterval, _ := strconv.Atoi(str(result, "interval"))
		if status == "slow_down" {
			interval = max(interval+5, returnedInterval)
		} else {
			interval = max(interval, returnedInterval)
		}
		if _, err := s.DB.Exec(ctx, `UPDATE upstream_oauth_sessions SET interval_seconds = $2, next_poll_at = $3, last_status = $4 WHERE upstream_id = $1`,
			upstream.ID, interval, s.now().Add(time.Duration(interval)*time.Second), status); err != nil {
			return nil, err
		}
		return &AuthorizationPoll{Status: status, Interval: interval}, nil
	case "expired", "denied":
		return s.endOAuth(ctx, upstream.ID, status)
	case "failed":
		return s.endOAuth(ctx, upstream.ID, "denied")
	case "success", "ready":
		tokenKey := "token_info"
		if spec.flowKind() == oauthInitPoll {
			tokenKey = "result"
		}
		updated, err := s.finishOAuth(ctx, upstream, spec, nested(tokenKey)(result), proxyID)
		if err != nil {
			return nil, err
		}
		s.auditOAuth(ctx, upstream, "poll-success")
		return &AuthorizationPoll{Status: "ready", Upstream: updated}, nil
	default:
		return nil, httpx.Unavailable("AUTH_POLL_FAILED", "authorization service returned an invalid status")
	}
}

func (s *Service) endOAuth(ctx context.Context, upstreamID int64, status string) (*AuthorizationPoll, error) {
	if _, err := s.DB.Exec(ctx, "DELETE FROM upstream_oauth_sessions WHERE upstream_id = $1", upstreamID); err != nil {
		return nil, err
	}
	return &AuthorizationPoll{Status: status}, nil
}

func (s *Service) auditOAuth(ctx context.Context, upstream *Upstream, action string) {
	detail, _ := json.Marshal(map[string]any{"platform": upstream.Platform, "auth_type": upstream.AuthType})
	if _, err := s.DB.Exec(ctx, `INSERT INTO audit_logs (actor_type, actor_id, action, target, detail) VALUES ('seller', $1, $2, $3, $4)`,
		upstream.SellerID, "upstream.oauth."+action, fmt.Sprintf("upstream:%d", upstream.ID), detail); err != nil && s.Log != nil {
		s.Log.Warn("oauth audit failed", "upstream_id", upstream.ID, "err", err)
	}
}
