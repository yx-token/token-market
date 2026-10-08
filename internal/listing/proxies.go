package listing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/sub2api/adminclient"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Proxy struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	Protocol           string    `json:"protocol"`
	Host               string    `json:"host"`
	Port               int       `json:"port"`
	Username           *string   `json:"username,omitempty"`
	Region             *string   `json:"region,omitempty"`
	Sub2APIProxyID     *int64    `json:"sub2api_proxy_id,omitempty"`
	Status             string    `json:"status"`
	AssignedUpstreamID *int64    `json:"assigned_upstream_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
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

const proxyColumns = `id, name, protocol, host, port, username, region, sub2api_proxy_id, status, assigned_upstream_id, created_at`

func scanProxy(row pgx.Row) (*Proxy, error) {
	var p Proxy
	err := row.Scan(&p.ID, &p.Name, &p.Protocol, &p.Host, &p.Port, &p.Username, &p.Region, &p.Sub2APIProxyID, &p.Status, &p.AssignedUpstreamID, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ProxyService 管理平台代理库存，并同步到 sub2api。
type ProxyService struct {
	DB      DB
	Admin   *adminclient.Client
	Keyring *crypto.Keyring
}

func (s *ProxyService) Create(ctx context.Context, in CreateProxyInput) (*Proxy, error) {
	switch in.Protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, httpx.BadRequest("INVALID_PROTOCOL", "protocol must be http, https, socks5 or socks5h")
	}
	if in.Name == "" || in.Host == "" || in.Port <= 0 || in.Port > 65535 {
		return nil, httpx.BadRequest("INVALID_PROXY", "name, host and port are required")
	}
	var passEnc []byte
	var id int64
	if err := s.DB.QueryRow(ctx, `INSERT INTO proxies (name, protocol, host, port, username, region)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, '')) RETURNING id`,
		in.Name, in.Protocol, in.Host, in.Port, in.Username, in.Region).Scan(&id); err != nil {
		return nil, err
	}
	if in.Password != "" {
		var err error
		if passEnc, err = s.Keyring.SealString(in.Password, fmt.Sprintf("proxy:%d", id)); err != nil {
			return nil, err
		}
		if _, err := s.DB.Exec(ctx, "UPDATE proxies SET password_enc = $2 WHERE id = $1", id, passEnc); err != nil {
			return nil, err
		}
	}
	if err := s.sync(ctx, id, in.Password); err != nil {
		return nil, fmt.Errorf("proxy saved but sync to sub2api failed (retry with sync): %w", err)
	}
	return s.Get(ctx, id)
}

func (s *ProxyService) Get(ctx context.Context, id int64) (*Proxy, error) {
	p, err := scanProxy(s.DB.QueryRow(ctx, "SELECT "+proxyColumns+" FROM proxies WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("PROXY_NOT_FOUND", "proxy not found")
	}
	return p, err
}

func (s *ProxyService) List(ctx context.Context) ([]Proxy, error) {
	rows, err := s.DB.Query(ctx, "SELECT "+proxyColumns+" FROM proxies ORDER BY id")
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

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
