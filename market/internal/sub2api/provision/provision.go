// Package provision 封装"幂等地确保某个 sub2api 资源存在"的操作。
//
// sub2api 只对部分创建接口支持幂等键（账号、代理），分组和用户的创建不支持，
// 所以这里统一采用"先按名称查只读库，没有再创建，创建失败再查一次"的方式，
// 配合 tm- 前缀且包含本系统 ID 的唯一名称，保证重试不会重复创建。
package provision

import (
	"context"
	"fmt"

	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
)

type Provisioner struct {
	Admin   *adminclient.Client
	Replica *replica.Reader
}

type GroupSpec struct {
	Name               string
	Platform           string
	RateMultiplier     float64
	Exclusive          bool
	ProfitControl      bool
	ProfitMinMargin    float64
	ProfitSafetyBuffer float64
	Description        string
}

// EnsureGroup 返回同名分组的 ID，不存在则创建。
func (p *Provisioner) EnsureGroup(ctx context.Context, spec GroupSpec) (int64, error) {
	if g, err := p.Replica.GroupByName(ctx, spec.Name); err != nil {
		return 0, err
	} else if g != nil {
		return g.ID, nil
	}
	in := adminclient.CreateGroupInput{
		Name:           spec.Name,
		Description:    spec.Description,
		Platform:       spec.Platform,
		RateMultiplier: spec.RateMultiplier,
		IsExclusive:    spec.Exclusive,
	}
	if spec.ProfitControl {
		margin, buffer := spec.ProfitMinMargin, spec.ProfitSafetyBuffer
		in.ProfitControlEnabled = true
		in.ProfitMinMargin = &margin
		in.ProfitSafetyBuffer = &buffer
	}
	g, err := p.Admin.CreateGroup(ctx, in)
	if err == nil {
		return g.ID, nil
	}
	// 创建可能已经成功但响应丢失：再查一次
	if existing, lookupErr := p.Replica.GroupByName(ctx, spec.Name); lookupErr == nil && existing != nil {
		return existing.ID, nil
	}
	return 0, fmt.Errorf("create group %s: %w", spec.Name, err)
}

// EnsureAccount 返回同名账号的 ID，不存在则用幂等键创建。
func (p *Provisioner) EnsureAccount(ctx context.Context, in adminclient.CreateAccountInput, idempotencyKey string) (int64, error) {
	if a, err := p.Replica.AccountByName(ctx, in.Name); err != nil {
		return 0, err
	} else if a != nil {
		return a.ID, nil
	}
	acc, err := p.Admin.CreateAccount(ctx, in, idempotencyKey)
	if err == nil {
		return acc.ID, nil
	}
	if existing, lookupErr := p.Replica.AccountByName(ctx, in.Name); lookupErr == nil && existing != nil {
		return existing.ID, nil
	}
	return 0, fmt.Errorf("create account %s: %w", in.Name, err)
}
