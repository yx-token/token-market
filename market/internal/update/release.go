// Package update 提供站内发布更新和多进程版本切换。
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultRepository = "yx-token/token-market"
const maxArchiveSize int64 = 500 << 20

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)

// Newer 仅允许升级至正式版本；同版本的预发布可以升级至正式版。
func Newer(target, current string) bool {
	if !versionPattern.MatchString(target) || !versionPattern.MatchString(current) || strings.Contains(target, "-") {
		return false
	}
	a := strings.Split(strings.TrimPrefix(target, "v"), ".")
	b := strings.Split(strings.SplitN(strings.TrimPrefix(current, "v"), "-", 2)[0], ".")
	for i := range a {
		x, ex := strconv.ParseUint(a[i], 10, 64)
		y, ey := strconv.ParseUint(b[i], 10, 64)
		if ex != nil || ey != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return strings.Contains(current, "-")
}

func ArchiveName() string {
	return fmt.Sprintf("token-market_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

type Asset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type Release struct {
	Version     string  `json:"version"`
	Name        string  `json:"name"`
	Notes       string  `json:"notes"`
	URL         string  `json:"url"`
	PublishedAt string  `json:"published_at"`
	Assets      []Asset `json:"-"`
	Compatible  bool    `json:"compatible"`
}

type Source interface {
	Latest(context.Context, bool) (*Release, error)
	Download(context.Context, Asset, string, int64) error
}

// GitHub 的仓库与下载地址只由服务端配置和 Release asset ID 决定。
type GitHub struct {
	Repository string
	Token      string
	Client     *http.Client
	mu         sync.Mutex
	cached     *Release
	cachedAt   time.Time
}

func NewGitHub(repository, token string) *GitHub {
	return &GitHub{Repository: repository, Token: token, Client: &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !trustedURL(req.URL) {
				return errors.New("更新下载重定向地址无效")
			}
			if req.URL.Host != "api.github.com" {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}}
}

func trustedURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Host {
	case "api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return true
	}
	return false
}

func (g *GitHub) get(ctx context.Context, path, accept string) (*http.Response, error) {
	if !repositoryPattern.MatchString(g.Repository) {
		return nil, errors.New("发布仓库配置无效，应为 owner/repository")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+g.Repository+path, nil)
	if err != nil {
		return nil, errors.New("发布地址配置无效")
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "token-market-updater")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, errors.New("无法连接 GitHub，请检查服务器网络")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		switch resp.StatusCode {
		case 404:
			return nil, errors.New("未找到正式 Release；私有仓库请配置具有 Contents 读取权限的 GitHub 令牌")
		case 401, 403, 429:
			return nil, errors.New("GitHub 访问受限，请检查令牌权限或稍后重试")
		default:
			return nil, fmt.Errorf("查询发布文件失败（HTTP %d）", resp.StatusCode)
		}
	}
	return resp, nil
}

func (g *GitHub) Latest(ctx context.Context, force bool) (*Release, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !force && g.cached != nil && time.Since(g.cachedAt) < 5*time.Minute {
		return g.cached, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := g.get(ctx, "/releases/latest", "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var remote struct {
		Tag         string  `json:"tag_name"`
		Name        string  `json:"name"`
		Body        string  `json:"body"`
		PublishedAt string  `json:"published_at"`
		Draft       bool    `json:"draft"`
		Prerelease  bool    `json:"prerelease"`
		Assets      []Asset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&remote); err != nil {
		return nil, errors.New("发布信息格式无效")
	}
	if !versionPattern.MatchString(remote.Tag) || strings.Contains(remote.Tag, "-") || remote.Draft || remote.Prerelease {
		return nil, errors.New("最新发布不是有效的正式版本")
	}
	r := &Release{Version: remote.Tag, Name: remote.Name, Notes: remote.Body, PublishedAt: remote.PublishedAt,
		URL: "https://github.com/" + g.Repository + "/releases/tag/" + remote.Tag, Assets: remote.Assets}
	r.Compatible = findAsset(r, ArchiveName()) != nil && findAsset(r, "checksums.txt") != nil
	g.cached, g.cachedAt = r, time.Now()
	return r, nil
}

func findAsset(r *Release, name string) *Asset {
	for _, a := range r.Assets {
		if a.Name == name && a.ID > 0 && a.Size > 0 {
			return &a
		}
	}
	return nil
}

func (g *GitHub) Download(ctx context.Context, asset Asset, dest string, limit int64) error {
	if asset.ID <= 0 || asset.Size <= 0 || asset.Size > limit {
		return errors.New("发布文件大小或标识无效")
	}
	resp, err := g.get(ctx, fmt.Sprintf("/releases/assets/%d", asset.ID), "application/octet-stream")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.ContentLength > limit {
		return errors.New("发布文件超过大小限制")
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	if n > limit || n != asset.Size {
		return errors.New("发布文件大小不匹配或下载不完整")
	}
	return nil
}
