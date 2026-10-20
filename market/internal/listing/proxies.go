package listing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

type Proxy struct {
	ID                  int64                           `json:"id"`
	SellerID            *int64                          `json:"seller_id,omitempty"`
	Name                string                          `json:"name"`
	Protocol            string                          `json:"protocol"`
	Host                string                          `json:"host"`
	Port                int                             `json:"port"`
	Username            *string                         `json:"username,omitempty"`
	Region              *string                         `json:"region,omitempty"`
	Sub2APIProxyID      *int64                          `json:"sub2api_proxy_id,omitempty"`
	Status              string                          `json:"status"`
	AssignedUpstreamID  *int64                          `json:"assigned_upstream_id,omitempty"`
	CreatedAt           time.Time                       `json:"created_at"`
	ConnectionReport    *adminclient.ProxyTestResult    `json:"connection_report,omitempty"`
	QualityReport       *adminclient.ProxyQualityResult `json:"quality_report,omitempty"`
	ConnectionCheckedAt *time.Time                      `json:"connection_checked_at,omitempty"`
	QualityCheckedAt    *time.Time                      `json:"quality_checked_at,omitempty"`
}

type CreateProxyInput struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Region   string `json:"region"`
}

const proxyColumns = `id, seller_id, name, protocol, host, port, username, region, sub2api_proxy_id, status, assigned_upstream_id, created_at, connection_report, quality_report, connection_checked_at, quality_checked_at`

func scanProxy(row pgx.Row) (*Proxy, error) {
	var p Proxy
	var connection, quality []byte
	err := row.Scan(&p.ID, &p.SellerID, &p.Name, &p.Protocol, &p.Host, &p.Port, &p.Username, &p.Region, &p.Sub2APIProxyID, &p.Status, &p.AssignedUpstreamID, &p.CreatedAt, &connection, &quality, &p.ConnectionCheckedAt, &p.QualityCheckedAt)
	if err != nil {
		return nil, err
	}
	if len(connection) > 0 {
		if err := json.Unmarshal(connection, &p.ConnectionReport); err != nil {
			return nil, err
		}
	}
	if len(quality) > 0 {
		if err := json.Unmarshal(quality, &p.QualityReport); err != nil {
			return nil, err
		}
	}
	return &p, nil
}

// ProxyService 管理平台和卖家代理库存，并同步到 sub2api。
type ProxyService struct {
	DB      DB
	Admin   *adminclient.Client
	Keyring *crypto.Keyring
}

func (s *ProxyService) Create(ctx context.Context, in CreateProxyInput) (*Proxy, error) {
	return s.create(ctx, nil, in)
}

func (s *ProxyService) CreateSeller(ctx context.Context, sellerID int64, in CreateProxyInput) (*Proxy, error) {
	return s.create(ctx, &sellerID, in)
}

// ensureSeller 用于导入账号时带入导出文件中的代理：卖家已登记同一地址时复用原记录（以库存中的配置为准），
// 否则新建并同步到 sub2api。地址已由其他卖家登记时拒绝。
func (s *ProxyService) ensureSeller(ctx context.Context, sellerID int64, in CreateProxyInput) (*Proxy, error) {
	in, err := normalizeProxyInput(in)
	if err != nil {
		return nil, err
	}
	p, err := scanProxy(s.DB.QueryRow(ctx, "SELECT "+proxyColumns+" FROM proxies WHERE seller_id IS NOT NULL AND lower(host) = $1 AND port = $2", in.Host, in.Port))
	if errors.Is(err, pgx.ErrNoRows) {
		return s.create(ctx, &sellerID, in)
	}
	if err != nil {
		return nil, err
	}
	if *p.SellerID != sellerID {
		return nil, httpx.Conflict("PROXY_ALREADY_EXISTS", "this proxy address is already registered")
	}
	if p.Sub2APIProxyID == nil {
		if p, err = s.Sync(ctx, p.ID); err != nil {
			return nil, httpx.Unavailable("PROXY_SYNC_FAILED", "proxy saved but sync failed; retry synchronization from the proxy list")
		}
	}
	return p, nil
}

var proxyHostname = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

// normalizeProxyInput 校验代理参数，并把地址规范成唯一索引使用的形式。
func normalizeProxyInput(in CreateProxyInput) (CreateProxyInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.ToLower(strings.TrimSpace(in.Host))
	in.Host = strings.TrimSuffix(strings.TrimPrefix(in.Host, "["), "]")
	in.Region = strings.TrimSpace(in.Region)
	switch in.Protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return in, httpx.BadRequest("INVALID_PROTOCOL", "protocol must be http, https, socks5 or socks5h")
	}
	if in.Name == "" || in.Host == "" || in.Port <= 0 || in.Port > 65535 {
		return in, httpx.BadRequest("INVALID_PROXY", "name, host and port are required")
	}
	if ip := net.ParseIP(in.Host); ip != nil {
		in.Host = ip.String()
	} else if len(in.Host) > 253 || !proxyHostname.MatchString(in.Host) || strings.Contains(in.Host, "..") {
		return in, httpx.BadRequest("INVALID_PROXY", "host must be an IP address or hostname, without a scheme or port")
	}
	return in, nil
}

func (s *ProxyService) create(ctx context.Context, sellerID *int64, in CreateProxyInput) (*Proxy, error) {
	in, err := normalizeProxyInput(in)
	if err != nil {
		return nil, err
	}
	var passEnc []byte
	var id int64
	if err := s.DB.QueryRow(ctx, `SELECT nextval('proxies_id_seq')`).Scan(&id); err != nil {
		return nil, err
	}
	if in.Password != "" {
		if passEnc, err = s.Keyring.SealString(in.Password, fmt.Sprintf("proxy:%d", id)); err != nil {
			return nil, err
		}
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO proxies (id, seller_id, name, protocol, host, port, username, region, password_enc)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), $9)`,
		id, sellerID, in.Name, in.Protocol, in.Host, in.Port, in.Username, in.Region, passEnc); err != nil {
		if isUnique(err) {
			return nil, httpx.Conflict("PROXY_ALREADY_EXISTS", "this proxy address is already registered")
		}
		return nil, err
	}
	if err := s.sync(ctx, id, in.Password); err != nil {
		return nil, httpx.Unavailable("PROXY_SYNC_FAILED", "proxy saved but sync failed; retry synchronization from the proxy list")
	}
	return s.Get(ctx, id)
}

func (s *ProxyService) GetSeller(ctx context.Context, sellerID, id int64) (*Proxy, error) {
	p, err := scanProxy(s.DB.QueryRow(ctx, "SELECT "+proxyColumns+" FROM proxies WHERE id = $1 AND seller_id = $2", id, sellerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("PROXY_NOT_FOUND", "proxy not found")
	}
	return p, err
}

func (s *ProxyService) Get(ctx context.Context, id int64) (*Proxy, error) {
	p, err := scanProxy(s.DB.QueryRow(ctx, "SELECT "+proxyColumns+" FROM proxies WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("PROXY_NOT_FOUND", "proxy not found")
	}
	return p, err
}

func (s *ProxyService) List(ctx context.Context) ([]Proxy, error) {
	return s.list(ctx, nil)
}

func (s *ProxyService) ListSeller(ctx context.Context, sellerID int64) ([]Proxy, error) {
	return s.list(ctx, &sellerID)
}

func (s *ProxyService) list(ctx context.Context, sellerID *int64) ([]Proxy, error) {
	rows, err := s.DB.Query(ctx, "SELECT "+proxyColumns+" FROM proxies WHERE ($1::bigint IS NULL OR seller_id = $1) ORDER BY id", sellerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Proxy
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *ProxyService) SyncSeller(ctx context.Context, sellerID, id int64) (*Proxy, error) {
	if _, err := s.GetSeller(ctx, sellerID, id); err != nil {
		return nil, err
	}
	return s.Sync(ctx, id)
}

func (s *ProxyService) SetSellerDisabled(ctx context.Context, sellerID, id int64, disabled bool) (*Proxy, error) {
	if _, err := s.GetSeller(ctx, sellerID, id); err != nil {
		return nil, err
	}
	return s.SetDisabled(ctx, id, disabled)
}

// Sync 把尚未同步的代理创建到 sub2api（幂等）。
func (s *ProxyService) Sync(ctx context.Context, id int64) (*Proxy, error) {
	var passEnc []byte
	if err := s.DB.QueryRow(ctx, "SELECT password_enc FROM proxies WHERE id = $1", id).Scan(&passEnc); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.NotFound("PROXY_NOT_FOUND", "proxy not found")
		}
		return nil, err
	}
	password := ""
	if len(passEnc) > 0 {
		var err error
		if password, err = s.Keyring.OpenString(passEnc, fmt.Sprintf("proxy:%d", id)); err != nil {
			return nil, err
		}
	}
	if err := s.sync(ctx, id, password); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *ProxyService) sync(ctx context.Context, id int64, password string) error {
	p, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if p.Sub2APIProxyID != nil {
		return nil
	}
	user := ""
	if p.Username != nil {
		user = *p.Username
	}
	created, err := s.Admin.CreateProxy(ctx, adminclient.CreateProxyInput{
		Name: fmt.Sprintf("tm-proxy-%d", p.ID), Protocol: p.Protocol, Host: p.Host, Port: p.Port, Username: user, Password: password,
	}, fmt.Sprintf("tm-proxy-%d", p.ID))
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, "UPDATE proxies SET sub2api_proxy_id = $2 WHERE id = $1", p.ID, created.ID)
	return err
}

// SetDisabled 停用或启用一个未分配的代理。
func (s *ProxyService) SetDisabled(ctx context.Context, id int64, disabled bool) (*Proxy, error) {
	status := "available"
	if disabled {
		status = "disabled"
	}
	tag, err := s.DB.Exec(ctx, `UPDATE proxies SET status = $2 WHERE id = $1 AND assigned_upstream_id IS NULL`, id, status)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.Conflict("PROXY_IN_USE", "proxy is assigned to an upstream or does not exist")
	}
	return s.Get(ctx, id)
}

// UpdateSeller 更新卖家未绑定账号的代理（名称/地区/账号密码；改主机端口会清掉 sub2api 同步以便重试）。
func (s *ProxyService) UpdateSeller(ctx context.Context, sellerID, id int64, in CreateProxyInput) (*Proxy, error) {
	p, err := s.GetSeller(ctx, sellerID, id)
	if err != nil {
		return nil, err
	}
	if p.AssignedUpstreamID != nil {
		return nil, httpx.Conflict("PROXY_IN_USE", "proxy is assigned to an upstream")
	}
	in, err = normalizeProxyInput(in)
	if err != nil {
		return nil, err
	}
	var passEnc []byte
	if in.Password != "" {
		passEnc, err = s.Keyring.SealString(in.Password, fmt.Sprintf("proxy:%d", id))
		if err != nil {
			return nil, err
		}
	}
	var user any
	if in.Username != "" {
		user = in.Username
	}
	var region any
	if in.Region != "" {
		region = in.Region
	}
	hostChanged := !strings.EqualFold(p.Host, in.Host) || p.Port != in.Port || p.Protocol != in.Protocol
	if in.Password != "" {
		_, err = s.DB.Exec(ctx, `UPDATE proxies SET name=$2, protocol=$3, host=$4, port=$5, username=$6, region=$7, password_enc=$8,
			sub2api_proxy_id = CASE WHEN $9 THEN NULL ELSE sub2api_proxy_id END
			WHERE id=$1 AND seller_id=$10 AND assigned_upstream_id IS NULL`,
			id, in.Name, in.Protocol, in.Host, in.Port, user, region, passEnc, hostChanged, sellerID)
	} else {
		_, err = s.DB.Exec(ctx, `UPDATE proxies SET name=$2, protocol=$3, host=$4, port=$5, username=$6, region=$7,
			sub2api_proxy_id = CASE WHEN $8 THEN NULL ELSE sub2api_proxy_id END
			WHERE id=$1 AND seller_id=$9 AND assigned_upstream_id IS NULL`,
			id, in.Name, in.Protocol, in.Host, in.Port, user, region, hostChanged, sellerID)
	}
	if err != nil {
		if isUnique(err) {
			return nil, httpx.Conflict("PROXY_EXISTS", "proxy address already registered")
		}
		return nil, err
	}
	out, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if out.Sub2APIProxyID == nil {
		_, _ = s.Sync(ctx, id) // 尽力同步；失败可在列表重试
	}
	return s.Get(ctx, id)
}

// DeleteSeller 删除卖家未绑定账号的代理，并尽量清理 sub2api 侧记录。
func (s *ProxyService) DeleteSeller(ctx context.Context, sellerID, id int64) error {
	p, err := s.GetSeller(ctx, sellerID, id)
	if err != nil {
		return err
	}
	if p.AssignedUpstreamID != nil {
		return httpx.Conflict("PROXY_IN_USE", "proxy is assigned to an upstream")
	}
	if p.Sub2APIProxyID != nil {
		_ = s.Admin.DeleteProxy(ctx, *p.Sub2APIProxyID)
	}
	tag, err := s.DB.Exec(ctx, `DELETE FROM proxies WHERE id = $1 AND seller_id = $2 AND assigned_upstream_id IS NULL`, id, sellerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.Conflict("PROXY_IN_USE", "proxy is assigned to an upstream or does not exist")
	}
	return nil
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
