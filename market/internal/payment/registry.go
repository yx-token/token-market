package payment

import "sort"

// Instance 是宿主支付服务商实例对应的客户端。停用只影响新订单。
type Instance struct {
	ID        int64
	Provider  Provider
	Enabled   bool
	SortOrder int
	Origin    string
}

// Registry 按实例保存客户端，按渠道选择排序最前的启用实例接收新订单。
// 发布给调用方前完成 SetEnabled / Disable，发布后只读。
type Registry struct {
	instances map[int64]Instance
	byName    map[string][]int64
	providers map[string]Provider // 兼容未使用宿主实例的协议测试与调用方
	disabled  map[string]bool
	enabled   bool
}

func NewRegistry(ps ...Provider) *Registry {
	r := NewInstanceRegistry()
	for _, p := range ps {
		if p != nil {
			r.providers[p.Name()] = p
		}
	}
	return r
}

func NewInstanceRegistry(instances ...Instance) *Registry {
	r := &Registry{instances: map[int64]Instance{}, byName: map[string][]int64{},
		providers: map[string]Provider{}, disabled: map[string]bool{}, enabled: true}
	for _, inst := range instances {
		if inst.ID > 0 && inst.Provider != nil {
			r.instances[inst.ID] = inst
		}
	}
	for id, inst := range r.instances {
		r.byName[inst.Provider.Name()] = append(r.byName[inst.Provider.Name()], id)
	}
	for _, ids := range r.byName {
		sort.Slice(ids, func(i, j int) bool {
			a, b := r.instances[ids[i]], r.instances[ids[j]]
			if a.SortOrder != b.SortOrder {
				return a.SortOrder < b.SortOrder
			}
			return a.ID < b.ID
		})
	}
	return r
}

// SetEnabled 设置宿主支付总开关，不影响历史订单访问客户端。
func (r *Registry) SetEnabled(enabled bool) {
	if r != nil {
		r.enabled = enabled
	}
}

// Disable 把整个渠道标记为停用（不再创建新订单）。
func (r *Registry) Disable(name string) {
	if r != nil {
		r.disabled[name] = true
	}
}

func (r *Registry) enabledDefault(name string) (Provider, int64) {
	for _, id := range r.byName[name] {
		if inst := r.instances[id]; inst.Enabled {
			return inst.Provider, id
		}
	}
	return r.providers[name], 0
}

// Default 返回可接收新订单的默认实例，支付总开关或渠道关闭时返回 nil。
func (r *Registry) Default(name string) (Provider, int64) {
	if r == nil || !r.enabled || r.disabled[name] {
		return nil, 0
	}
	return r.enabledDefault(name)
}

// Select 返回支持指定支付方式的最前启用实例。
func (r *Registry) Select(channel string) (Provider, int64) {
	name := ProviderOf(channel)
	if r == nil || !r.enabled || r.disabled[name] {
		return nil, 0
	}
	supports := func(p Provider) bool {
		for _, candidate := range p.Channels() {
			if candidate == channel {
				return true
			}
		}
		return false
	}
	for _, id := range r.byName[name] {
		if inst := r.instances[id]; inst.Enabled && supports(inst.Provider) {
			return inst.Provider, id
		}
	}
	if p := r.providers[name]; p != nil && supports(p) {
		return p, 0
	}
	return nil, 0
}

// Get 返回渠道当前默认客户端（含停用的），用于未记录实例 ID 的历史订单。
// 全部实例已停用时仍返回排序最前的实例，以保留原 Disable 语义。
func (r *Registry) Get(name string) Provider {
	if r == nil {
		return nil
	}
	if p, _ := r.enabledDefault(name); p != nil {
		return p
	}
	if ids := r.byName[name]; len(ids) > 0 {
		return r.instances[ids[0]].Provider
	}
	return nil
}

// GetInstance 返回订单指定的实例。只有 id=0 的历史订单回退到当前默认实例。
func (r *Registry) GetInstance(name string, id int64) Provider {
	if id == 0 {
		return r.Get(name)
	}
	if r != nil {
		if inst, ok := r.instances[id]; ok && inst.Provider.Name() == name {
			return inst.Provider
		}
	}
	return nil
}

// Origin 返回实例的通知源站地址。id=0 时与 Get 使用相同的历史订单回退。
func (r *Registry) Origin(name string, id int64) string {
	if r == nil {
		return ""
	}
	if id == 0 {
		_, id = r.enabledDefault(name)
		if id == 0 && len(r.byName[name]) > 0 {
			id = r.byName[name][0]
		}
	}
	if inst, ok := r.instances[id]; ok && inst.Provider.Name() == name {
		return inst.Origin
	}
	return ""
}

func (r *Registry) Enabled(name string) bool {
	p, _ := r.Default(name)
	return p != nil
}

// Configured 列出启用实例可接收的新订单方式（不含人工确认）。
func (r *Registry) Configured() map[string]bool {
	out := map[string]bool{}
	for _, channel := range AllChannels {
		if p, _ := r.Select(channel); p != nil {
			out[channel] = true
		}
	}
	return out
}
