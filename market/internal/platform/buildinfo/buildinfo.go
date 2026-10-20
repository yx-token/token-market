// Package buildinfo 记录构建时注入的版本信息：发布流程用
// -ldflags "-X github.com/yx-token/token-market/market/internal/platform/buildinfo.Version=v1.2.3 -X ...Commit=<sha>" 写入。
package buildinfo

var (
	// Version 是发布版本（git tag，例如 v0.3.0）；本地构建为 dev。
	Version = "dev"
	// Commit 是构建所用的 git 提交。
	Commit = ""
)
