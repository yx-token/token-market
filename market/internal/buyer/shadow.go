// Package buyer 管理买家侧：sub2api 影子用户、买家 Key、内部 key，以及发布给 Edge 的路由配置。
package buyer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
	"github.com/yx-token/token-market/market/internal/platform/randx"
	"github.com/yx-token/token-market/market/internal/settings"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
	"github.com/yx-token/token-market/market/internal/sub2api/userclient"
)

const (
	KindBuyer = "buyer"
	KindProbe = "probe"
)

// Identity 是本系统在 sub2api 中的一个用户身份。
type Identity struct {
	ID              int64
	Kind            string
	OwnerID         int64
	Status          string
	Sub2APIUserID   *int64
	Email           string
	AllowedGroupIDs []int64
	KeysLocked      bool
}

type ShadowService struct {
	DB       *pgxpool.Pool
	Admin    *adminclient.Client
	User     *userclient.Client
	Replica  *replica.Reader
	Keyring  *crypto.Keyring
	Domain   string
	Settings func(ctx context.Context) (settings.Settings, error)
	Log      *slog.Logger
	Now      func() time.Time
}

func (s *ShadowService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// aad 用身份的 kind 与 owner 绑定密文，加密时不依赖数据库自增 ID。
func aad(kind string, ownerID int64, field string) string {
	return fmt.Sprintf("shadow:%s:%d:%s", kind, ownerID, field)
}

func (s *ShadowService) load(ctx context.Context, kind string, ownerID int64) (*Identity, error) {
	var it Identity
	err := s.DB.QueryRow(ctx, `SELECT id, kind, owner_id, status, sub2api_user_id, email, allowed_group_ids, keys_locked
		FROM shadow_identities WHERE kind = $1 AND owner_id = $2`, kind, ownerID).
		Scan(&it.ID, &it.Kind, &it.OwnerID, &it.Status, &it.Sub2APIUserID, &it.Email, &it.AllowedGroupIDs, &it.KeysLocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &it, err
}

// Get 返回已存在的身份，不存在时返回 nil。
func (s *ShadowService) Get(ctx context.Context, kind string, ownerID int64) (*Identity, error) {
	return s.load(ctx, kind, ownerID)
}

// Ensure 确保影子用户存在且已登录。密码在调用 sub2api 前先落库，失败重试时可以沿用。
func (s *ShadowService) Ensure(ctx context.Context, kind string, ownerID int64) (*Identity, error) {
	it, err := s.load(ctx, kind, ownerID)
	if err != nil {
		return nil, err
	}
	if it != nil && it.Status == "active" {
		return it, nil
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	if it == nil {
		email := fmt.Sprintf("%s-%d@%s", kind, ownerID, s.Domain)
		enc, err := s.Keyring.SealString(randomString(32), aad(kind, ownerID, "password"))
		if err != nil {
			return nil, err
		}
		// 并发创建时以先写入的密码为准
		if _, err := s.DB.Exec(ctx, `INSERT INTO shadow_identities (kind, owner_id, email, password_enc, concurrency)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT (kind, owner_id) DO NOTHING`,
			kind, ownerID, email, enc, st.ShadowConcurrency); err != nil {
			return nil, err
		}
		if it, err = s.load(ctx, kind, ownerID); err != nil {
			return nil, err
		}
	}
	password, err := s.password(ctx, it)
	if err != nil {
		return nil, err
	}
	if it.Sub2APIUserID == nil {
		userID, err := s.createUser(ctx, it, password, st.ShadowConcurrency)
		if err != nil {
			s.markFailed(ctx, it.ID, err)
			return nil, err
		}
		if _, err := s.DB.Exec(ctx, "UPDATE shadow_identities SET sub2api_user_id = $2, updated_at = now() WHERE id = $1", it.ID, userID); err != nil {
			return nil, err
		}
		it.Sub2APIUserID = &userID
	}
	if err := s.login(ctx, it, password); err != nil {
		s.markFailed(ctx, it.ID, err)
		return nil, err
	}
	if _, err := s.DB.Exec(ctx, "UPDATE shadow_identities SET status = 'active', last_error = NULL, updated_at = now() WHERE id = $1", it.ID); err != nil {
		return nil, err
	}
	return s.load(ctx, kind, ownerID)
}

func (s *ShadowService) createUser(ctx context.Context, it *Identity, password string, concurrency int) (int64, error) {
	if u, err := s.Replica.UserByEmail(ctx, it.Email); err != nil {
		return 0, err
	} else if u != nil {
		return u.ID, nil
	}
	in := adminclient.CreateUserInput{
		Email:       it.Email,
		Password:    password,
		Username:    fmt.Sprintf("tm-%s-%d", it.Kind, it.OwnerID),
		Notes:       fmt.Sprintf("tm:shadow:%s:%d", it.Kind, it.OwnerID),
		Concurrency: concurrency,
	}
	if it.Kind == KindCreditPack {
		// 省略 balance 会继承 sub2api 默认赠金；活动钱包只能持有已购额度。
		zero := 0.0
		in.Balance = &zero
	}
	u, err := s.Admin.CreateUser(ctx, in)
	if err == nil {
		return u.ID, nil
	}
	if existing, lookupErr := s.Replica.UserByEmail(ctx, it.Email); lookupErr == nil && existing != nil {
		return existing.ID, nil
	}
	return 0, fmt.Errorf("create shadow user: %w", err)
}

func (s *ShadowService) password(ctx context.Context, it *Identity) (string, error) {
	var enc []byte
	if err := s.DB.QueryRow(ctx, "SELECT password_enc FROM shadow_identities WHERE id = $1", it.ID).Scan(&enc); err != nil {
		return "", err
	}
	return s.Keyring.OpenString(enc, aad(it.Kind, it.OwnerID, "password"))
}

func (s *ShadowService) login(ctx context.Context, it *Identity, password string) error {
	tok, err := s.User.Login(ctx, it.Email, password)
	if err != nil {
		return fmt.Errorf("shadow login: %w", err)
	}
	return s.saveTokens(ctx, it, tok)
}

func (s *ShadowService) saveTokens(ctx context.Context, it *Identity, tok *userclient.Tokens) error {
	access, err := s.Keyring.SealString(tok.AccessToken, aad(it.Kind, it.OwnerID, "access"))
	if err != nil {
		return err
	}
	refresh, err := s.Keyring.SealString(tok.RefreshToken, aad(it.Kind, it.OwnerID, "refresh"))
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `UPDATE shadow_identities SET access_token_enc = $2, refresh_token_enc = $3, access_expires_at = $4, updated_at = now()
		WHERE id = $1`, it.ID, access, refresh, tok.ExpiresAt(s.now()))
	return err
}

func (s *ShadowService) markFailed(ctx context.Context, id int64, err error) {
	_, _ = s.DB.Exec(ctx, "UPDATE shadow_identities SET status = 'failed', last_error = $2, updated_at = now() WHERE id = $1", id, err.Error())
}

// AccessToken 返回可用的用户令牌：缓存有效则直接用，否则刷新，刷新失败再重新登录。
func (s *ShadowService) AccessToken(ctx context.Context, it *Identity) (string, error) {
	var accessEnc, refreshEnc []byte
	var expires *time.Time
	if err := s.DB.QueryRow(ctx, "SELECT access_token_enc, refresh_token_enc, access_expires_at FROM shadow_identities WHERE id = $1", it.ID).
		Scan(&accessEnc, &refreshEnc, &expires); err != nil {
		return "", err
	}
	if len(accessEnc) > 0 && expires != nil && s.now().Add(2*time.Minute).Before(*expires) {
		return s.Keyring.OpenString(accessEnc, aad(it.Kind, it.OwnerID, "access"))
	}
	if len(refreshEnc) > 0 {
		refresh, err := s.Keyring.OpenString(refreshEnc, aad(it.Kind, it.OwnerID, "refresh"))
		if err != nil {
			return "", err
		}
		if tok, err := s.User.Refresh(ctx, refresh); err == nil {
			if err := s.saveTokens(ctx, it, tok); err != nil {
				return "", err
			}
			return tok.AccessToken, nil
		}
		s.Log.Warn("shadow token refresh failed, logging in again", "identity_id", it.ID)
	}
	password, err := s.password(ctx, it)
	if err != nil {
		return "", err
	}
	tok, err := s.User.Login(ctx, it.Email, password)
	if err != nil {
		return "", fmt.Errorf("shadow login: %w", err)
	}
	if err := s.saveTokens(ctx, it, tok); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// AllowGroup 把分组加入影子用户的可用分组（独占分组只有被授权的用户才能绑定 key）。
// 在事务中锁住身份行再调用 sub2api，避免并发添加时互相覆盖。
func (s *ShadowService) AllowGroup(ctx context.Context, identityID, groupID int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID *int64
	var allowed []int64
	if err := tx.QueryRow(ctx, "SELECT sub2api_user_id, allowed_group_ids FROM shadow_identities WHERE id = $1 FOR UPDATE", identityID).
		Scan(&userID, &allowed); err != nil {
		return err
	}
	if slices.Contains(allowed, groupID) {
		return nil
	}
	if userID == nil {
		return errors.New("shadow user not provisioned")
	}
	next := append(append([]int64(nil), allowed...), groupID)
	if _, err := s.Admin.UpdateUser(ctx, *userID, adminclient.UpdateUserInput{AllowedGroups: &next}); err != nil {
		return fmt.Errorf("allow group %d: %w", groupID, err)
	}
	if _, err := tx.Exec(ctx, "UPDATE shadow_identities SET allowed_group_ids = $2, updated_at = now() WHERE id = $1", identityID, next); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreateKey 以影子用户身份创建 sub2api key；同一 custom_key 已存在时返回已有记录（重试幂等）。
func (s *ShadowService) CreateKey(ctx context.Context, it *Identity, groupID int64, name, customKey string) (int64, error) {
	token, err := s.AccessToken(ctx, it)
	if err != nil {
		return 0, err
	}
	k, err := s.User.CreateKey(ctx, token, userclient.CreateKeyInput{Name: name, GroupID: groupID, CustomKey: customKey})
	if err == nil {
		return k.ID, nil
	}
	if existing, lookupErr := s.Replica.APIKeyByKey(ctx, customKey); lookupErr == nil && existing != nil {
		return existing.ID, nil
	}
	return 0, fmt.Errorf("create internal key: %w", err)
}

// SetKeyStatus 启用或停用 sub2api key（冻结可疑的内部 key）。
func (s *ShadowService) SetKeyStatus(ctx context.Context, it *Identity, keyID int64, active bool) error {
	token, err := s.AccessToken(ctx, it)
	if err != nil {
		return err
	}
	status := "inactive"
	if active {
		status = "active"
	}
	_, err = s.User.UpdateKey(ctx, token, keyID, userclient.UpdateKeyInput{Status: status})
	return err
}

// DeleteKey 删除 sub2api key。
func (s *ShadowService) DeleteKey(ctx context.Context, it *Identity, keyID int64) error {
	token, err := s.AccessToken(ctx, it)
	if err != nil {
		return err
	}
	return s.User.DeleteKey(ctx, token, keyID)
}

// EnsureSameEmailUser 按邮箱复用引擎用户；不存在则用本次密码创建 role=user。
func (s *ShadowService) EnsureSameEmailUser(ctx context.Context, email, password string) (int64, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(strings.TrimSpace(password)) < 6 {
		return 0, fmt.Errorf("email and password(min 6) required")
	}
	if s.Replica == nil || s.Admin == nil {
		return 0, fmt.Errorf("engine admin unavailable")
	}
	if u, err := s.Replica.UserByEmail(ctx, email); err != nil {
		return 0, err
	} else if u != nil {
		return u.ID, nil
	}
	username := strings.Split(email, "@")[0]
	if username == "" {
		username = "user"
	}
	created, err := s.Admin.CreateUser(ctx, adminclient.CreateUserInput{
		Email: email, Password: password, Username: username,
		Notes: "tm:market:" + email, Concurrency: 5,
	})
	if err == nil {
		return created.ID, nil
	}
	if u, lookupErr := s.Replica.UserByEmail(ctx, email); lookupErr == nil && u != nil {
		return u.ID, nil
	}
	return 0, fmt.Errorf("create engine user: %w", err)
}

// BindLinkedBuyer 把买家影子指到已存在的引擎用户（同一邮箱），不再另开 buyer-{id}@domain。
// 已绑定到其他引擎用户时拒绝覆盖。密码按影子 AAD 加密，供后续 AccessToken 登录。
func (s *ShadowService) BindLinkedBuyer(ctx context.Context, ownerID, engineUserID int64, email, password string) (*Identity, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	password = strings.TrimSpace(password)
	if ownerID <= 0 || engineUserID <= 0 || email == "" || len(password) < 6 {
		return nil, fmt.Errorf("owner, engine user, email and password(min 6) required")
	}
	if s.Keyring == nil || s.DB == nil || s.User == nil {
		return nil, fmt.Errorf("shadow store unavailable")
	}
	concurrency := 5
	if s.Settings != nil {
		st, err := s.Settings(ctx)
		if err != nil {
			return nil, err
		}
		if st.ShadowConcurrency > 0 {
			concurrency = st.ShadowConcurrency
		}
	}
	enc, err := s.Keyring.SealString(password, aad(KindBuyer, ownerID, "password"))
	if err != nil {
		return nil, err
	}
	existing, err := s.load(ctx, KindBuyer, ownerID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Sub2APIUserID != nil && *existing.Sub2APIUserID != engineUserID {
		return nil, fmt.Errorf("buyer shadow already bound to engine user %d", *existing.Sub2APIUserID)
	}
	if existing == nil {
		_, err = s.DB.Exec(ctx, `INSERT INTO shadow_identities
			(kind, owner_id, status, sub2api_user_id, email, password_enc, concurrency)
			VALUES ('buyer', $1, 'provisioning', $2, $3, $4, $5)`,
			ownerID, engineUserID, email, enc, concurrency)
	} else {
		tag, err2 := s.DB.Exec(ctx, `UPDATE shadow_identities
			SET email = $2, password_enc = $3, sub2api_user_id = $4, status = 'provisioning', last_error = NULL, concurrency = $5, updated_at = now()
			WHERE id = $1 AND (sub2api_user_id IS NULL OR sub2api_user_id = $4)`,
			existing.ID, email, enc, engineUserID, concurrency)
		if err2 != nil {
			return nil, err2
		}
		if tag.RowsAffected() == 0 {
			return nil, fmt.Errorf("buyer shadow was not updated")
		}
		err = nil
	}
	if err != nil {
		return nil, err
	}
	it, err := s.load(ctx, KindBuyer, ownerID)
	if err != nil {
		return nil, err
	}
	if err := s.login(ctx, it, password); err != nil {
		s.markFailed(ctx, it.ID, err)
		return nil, err
	}
	if _, err := s.DB.Exec(ctx, `UPDATE shadow_identities SET status = 'active', last_error = NULL, updated_at = now() WHERE id = $1`, it.ID); err != nil {
		return nil, err
	}
	return s.load(ctx, KindBuyer, ownerID)
}

func randomString(n int) string { return randx.String(n) }
