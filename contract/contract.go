// Package contract 描述 token-market 对 sub2api 的依赖：表与列、jsonb 键、枚举、接口、配置，
// 以及已验证兼容的版本。数据文件内嵌在二进制中，s2a-compat 与运行时自检共用。
package contract

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed contract.yaml queries/*.sql
var files embed.FS

type Spec struct {
	Version         int                     `yaml:"version"`
	Sub2API         Sub2APISpec             `yaml:"sub2api"`
	Tables          map[string]TableSpec    `yaml:"tables"`
	JSONBKeys       map[string]JSONBKeySpec `yaml:"jsonb_keys"`
	Enums           map[string][]string     `yaml:"enums"`
	AdminAPI        []EndpointSpec          `yaml:"admin_api"`
	UserAPI         []EndpointSpec          `yaml:"user_api"`
	Config          map[string]any          `yaml:"config"`
	ResponseHeaders map[string]string       `yaml:"response_headers"`
}

type Sub2APISpec struct {
	VerifiedVersions []VerifiedVersion `yaml:"verified_versions"`
}

type VerifiedVersion struct {
	Version               string   `yaml:"version"`
	MigrationFingerprints []string `yaml:"migration_fingerprints"`
}

type TableSpec struct {
	Large   bool     `yaml:"large"`
	UsedBy  []string `yaml:"used_by"`
	Columns []string `yaml:"columns"`
}

type JSONBKeySpec struct {
	Read  []string `yaml:"read"`
	Write []string `yaml:"write"`
}

type EndpointSpec struct {
	Method         string   `yaml:"method"`
	Path           string   `yaml:"path"`
	Request        []string `yaml:"request"`
	IdempotencyKey string   `yaml:"idempotency_key"`
}

// Load 解析内嵌的 contract.yaml。
func Load() (*Spec, error) {
	raw, err := files.ReadFile("contract.yaml")
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse 解析契约内容，供测试或 --contract 覆盖使用。
func Parse(raw []byte) (*Spec, error) {
	var s Spec
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse contract: %w", err)
	}
	if s.Version != 1 {
		return nil, fmt.Errorf("unsupported contract version %d", s.Version)
	}
	for name, t := range s.Tables {
		if len(t.Columns) == 0 {
			return nil, fmt.Errorf("contract table %s has no columns", name)
		}
	}
	return &s, nil
}

// Table 按名称（不区分大小写）查找契约表。
func (s *Spec) Table(name string) (TableSpec, bool) {
	t, ok := s.Tables[strings.ToLower(name)]
	return t, ok
}

// HasColumn 判断契约是否依赖 table.column。
func (s *Spec) HasColumn(table, column string) bool {
	t, ok := s.Table(table)
	if !ok {
		return false
	}
	column = strings.ToLower(column)
	for _, c := range t.Columns {
		if c == column {
			return true
		}
	}
	return false
}

// Verified 返回版本的验证记录。
func (s *Spec) Verified(version string) (VerifiedVersion, bool) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	for _, v := range s.Sub2API.VerifiedVersions {
		if strings.TrimPrefix(v.Version, "v") == version {
			return v, true
		}
	}
	return VerifiedVersion{}, false
}

// Queries 返回内嵌的只读查询，键为文件名去掉 .sql。
func Queries() (map[string]string, error) {
	names, err := fs.Glob(files, "queries/*.sql")
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(names))
	for _, name := range names {
		raw, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		key := strings.TrimSuffix(strings.TrimPrefix(name, "queries/"), ".sql")
		out[key] = string(raw)
	}
	return out, nil
}

// MigrationFingerprint 计算迁移文件名集合的指纹：排序后按换行拼接取 SHA-256。
// 只看文件名，文件内容的变化由 sub2api 自身的 checksum 校验负责。
func MigrationFingerprint(filenames []string) string {
	sorted := append([]string(nil), filenames...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
