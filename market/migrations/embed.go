// Package migrations 内嵌本系统数据库的迁移脚本，由 internal/platform/db.Migrate 执行。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
