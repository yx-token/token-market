// Package app 按配置组装各进程共用的依赖与业务服务，避免 api 与 worker 各自重复接线。
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/DueGin/token-market/internal/account"
	"github.com/DueGin/token-market/internal/billing"
	"github.com/DueGin/token-market/internal/buyer"
	"github.com/DueGin/token-market/internal/edgecfg"
	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/market"
	"github.com/DueGin/token-market/internal/payment/merchants"
	"github.com/DueGin/token-market/internal/platform/auth"
	"github.com/DueGin/token-market/internal/platform/config"
	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/platform/db"
	"github.com/DueGin/token-market/internal/quality"
	"github.com/DueGin/token-market/internal/risk"
	"github.com/DueGin/token-market/internal/scheduling"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/DueGin/token-market/internal/sub2api/adminclient"
	"github.com/DueGin/token-market/internal/sub2api/provision"
	"github.com/DueGin/token-market/internal/sub2api/replica"
	"github.com/DueGin/token-market/internal/sub2api/userclient"
	"github.com/DueGin/token-market/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type App struct {
	Cfg         *config.Config
	Log         *slog.Logger
	DB          *pgxpool.Pool
	ReplicaPool *pgxpool.Pool
	Replica     *replica.Reader
	Redis       *redis.Client
	Keyring     *crypto.Keyring
	Issuer      *auth.Issuer
	Payments    *merchants.Store
	Admin       *adminclient.Client
	User        *userclient.Client
	Prov        *provision.Provisioner
	Kicker      scheduling.RedisKicker
	Settings    func(ctx context.Context) (settings.Settings, error)

	Accounts   *account.Service
	Listings   *listing.Service
	Proxies    *listing.ProxyService
	Pools      *scheduling.PoolService
	Shadow     *buyer.ShadowService
	Keys       *buyer.KeyService
	Funds      *billing.Funds
	Market     *market.Service
	Quality    *quality.Engine
	Risk       *risk.Service
	Ingester   *billing.Ingester
	Reconciler *billing.Reconciler
	Limits     *billing.LimitEnforcer
	Scheduler  *scheduling.Runner
}

// Validate 检查 api 与 worker 都必需的配置。
func Validate(cfg *config.Config) error {
	switch {
	case cfg.Database.DSN == "":
		return errors.New("database.dsn is required")
	case cfg.Sub2API.BaseURL == "" || cfg.Sub2API.AdminAPIKey == "":
		return errors.New("sub2api.base_url and sub2api.admin_api_key are required")
	case cfg.Sub2API.ReplicaDSN == "":
		return errors.New("sub2api.replica_dsn is required")
	case cfg.Redis.Addr == "":
		return errors.New("redis.addr is required")
	case cfg.Crypto.ActiveKeyID == "":
		return errors.New("crypto.active_key_id and crypto.keys are required")
	case cfg.Auth.JWTSecret == "":
		return errors.New("auth.jwt_secret is required")
	}
	if cfg.Env == "prod" && cfg.Crypto.HashPepper == "" {
		return errors.New("crypto.hash_pepper is required in prod")
	}
	return nil
}

// Build 打开数据库、执行本系统迁移并组装全部服务。调用方负责 Close。
func Build(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	a := &App{Cfg: cfg, Log: log}
	ok := false
	defer func() {
		if !ok {
			a.Close()
		}
	}()

	var err error
	if a.Keyring, err = crypto.NewKeyring(cfg.Crypto); err != nil {
		return nil, err
	}
	if a.Issuer, err = auth.NewIssuer(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL); err != nil {
		return nil, err
	}
	if a.DB, err = db.Open(ctx, cfg.Database.DSN, cfg.Database.MaxConns, cfg.Database.TimeZone); err != nil {
		return nil, err
	}
	applied, err := db.Migrate(ctx, a.DB, migrations.FS)
	if err != nil {
		return nil, err
	}
	if len(applied) > 0 {
		log.Info("applied market migrations", "files", applied)
	}
	if a.ReplicaPool, err = db.Open(ctx, cfg.Sub2API.ReplicaDSN, 8, ""); err != nil {
		return nil, err
	}
	if a.Replica, err = replica.New(a.ReplicaPool); err != nil {
		return nil, err
	}
	a.Redis = redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	if err := a.Redis.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	gate := &compatGate{db: a.DB, ttl: 5 * time.Second}
	a.Admin = adminclient.New(cfg.Sub2API.BaseURL, cfg.Sub2API.AdminAPIKey, cfg.Sub2API.Timeout, adminclient.WithWriteGate(gate.Check))
	a.User = userclient.New(cfg.Sub2API.BaseURL, cfg.Sub2API.Timeout, nil)
	a.Prov = &provision.Provisioner{Admin: a.Admin, Replica: a.Replica}
	a.Kicker = scheduling.RedisKicker{Redis: a.Redis}
	a.Settings = func(ctx context.Context) (settings.Settings, error) { return settings.Load(ctx, a.DB) }
	a.Payments = &merchants.Store{DB: a.DB, Keyring: a.Keyring, Settings: a.Settings, Log: log}
	edgeStore := edgecfg.Store{Redis: a.Redis}

	a.Accounts = account.NewService(a.DB, a.Keyring, a.Issuer)
	a.Risk = &risk.Service{DB: a.DB, Settings: a.Settings}
	a.Proxies = &listing.ProxyService{DB: a.DB, Admin: a.Admin, Keyring: a.Keyring}
	a.Pools = &scheduling.PoolService{DB: a.DB, Prov: a.Prov}
	a.Listings = &listing.Service{
		DB: a.DB, Admin: a.Admin, Prov: a.Prov, Keyring: a.Keyring, Kicker: a.Kicker, Settings: a.Settings, Log: log,
	}
	a.Shadow = &buyer.ShadowService{
		DB: a.DB, Admin: a.Admin, User: a.User, Replica: a.Replica, Keyring: a.Keyring,
		Domain: cfg.Sub2API.ShadowEmailDomain, Settings: a.Settings, Log: log,
	}
	a.Keys = &buyer.KeyService{
		DB: a.DB, Shadow: a.Shadow, Prov: a.Prov, Keyring: a.Keyring, Edge: edgeStore, Kicker: a.Kicker, Settings: a.Settings, Log: log,
	}
	a.Funds = &billing.Funds{
		DB: a.DB, Replica: a.Replica, Shadow: a.Shadow, Keys: a.Keys, Keyring: a.Keyring,
		KYC: a.Accounts, Risk: a.Risk, Settings: a.Settings, Payments: a.Payments, Log: log,
	}
	a.Market = &market.Service{DB: a.DB, Commission: func(ctx context.Context) (float64, error) {
		st, err := a.Settings(ctx)
		return st.CommissionRate, err
	}, Prices: &market.PriceBook{Admin: a.Admin, Models: func(ctx context.Context) ([]string, error) {
		st, err := a.Settings(ctx)
		return st.PriceModels, err
	}}}
	a.Quality = &quality.Engine{
		DB: a.DB, Replica: a.Replica, Shadow: a.Shadow, Keyring: a.Keyring,
		Prober:   &quality.Prober{GatewayURL: cfg.Sub2API.GatewayURL, Client: &http.Client{}},
		Listings: a.Listings, Kicker: a.Kicker, Settings: a.Settings, Log: log, Concurrency: cfg.Worker.ProbeConcurrency,
		Edge: edgeStore, EdgePublicURL: cfg.Edge.PublicURL,
	}
	// 上架检测由质量引擎负责；两者互相引用，构造后再连接
	a.Listings.Quality = a.Quality
	a.Ingester = &billing.Ingester{DB: a.DB, Replica: a.Replica, Log: log}
	a.Reconciler = &billing.Reconciler{DB: a.DB, Replica: a.Replica, Shadow: a.Shadow, Settings: a.Settings, Log: log}
	a.Limits = &billing.LimitEnforcer{DB: a.DB, Keys: a.Keys, Log: log}
	a.Scheduler = &scheduling.Runner{DB: a.DB, Replica: a.Replica, Redis: a.Redis, Settings: a.Settings, Log: log}
	ok = true
	return a, nil
}

func (a *App) Close() {
	if a.Redis != nil {
		_ = a.Redis.Close()
	}
	if a.ReplicaPool != nil {
		a.ReplicaPool.Close()
	}
	if a.DB != nil {
		a.DB.Close()
	}
}
