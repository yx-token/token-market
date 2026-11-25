package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yx-token/token-market/market/internal/platform/buildinfo"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

// 浏览器另存为名称（用户可见）；GitHub Release / 镜像内仍用 ASCII 资产名，便于代理与 CI。
const (
	helperDownloadMacOS   = "闲猫助手.zip"
	helperDownloadWindows = "闲猫助手.exe"
	helperDownloadLinux   = "闲猫助手.AppImage"

	helperAssetMacOS      = "xianmao-helper-macos.zip"
	helperAssetWindows    = "xianmao-helper-windows-setup.exe"
	helperAssetWindowsZip = "xianmao-helper-windows-x64.zip"
	helperAssetLinux      = "xianmao-helper-linux-x64.AppImage"
)

var (
	// zip 打包会暂时摘掉 Resources/app 符号链接；launch 会 sync/打开同一棵树。必须同一把锁。
	helperAppMu sync.Mutex
)

func helperDistReady(dir string) bool {
	if dir == "" {
		return false
	}
	dist := filepath.Join(dir, "dist")
	if st, err := os.Stat(dist); err == nil && st.IsDir() {
		return true
	}
	// 允许直接把安装包放在 dir/dist 同级探测：若已有任一稳定文件名也算就绪
	for _, name := range []string{
		helperAssetMacOS,
		helperAssetWindows,
		helperDownloadWindows,
		helperAssetWindowsZip,
		helperAssetLinux,
		helperDownloadMacOS,
		helperDownloadLinux,
	} {
		if firstExistingFile(filepath.Join(dist, name), filepath.Join(dir, name)) != "" {
			return true
		}
	}
	return false
}

func helperDesktopDir() string {
	if d := strings.TrimSpace(os.Getenv("TM_HELPER_DIR")); d != "" {
		return d
	}
	candidates := make([]string, 0, 4)
	// 镜像：TM_HTTP_WEB_DIR=/app/web → /app/helper（见 market/Dockerfile）
	// 源码开发：market/web/dist → market/desktop/xianmao-helper
	if web := strings.TrimSpace(os.Getenv("TM_HTTP_WEB_DIR")); web != "" {
		candidates = append(candidates,
			filepath.Clean(filepath.Join(web, "..", "helper")),
			filepath.Clean(filepath.Join(web, "..", "..", "desktop", "xianmao-helper")),
		)
	}
	_, file, _, ok := runtime.Caller(0)
	if ok {
		candidates = append(candidates,
			filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "desktop", "xianmao-helper")),
		)
	}
	for _, c := range candidates {
		if helperDistReady(c) {
			return c
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

func helperAppPath(dir string) string {
	return filepath.Join(dir, "dist", "闲猫助手.app")
}

func helperZipPath(dir string) string {
	// 优先稳定别名（scripts/pack-release.sh）；兼容开发态旧名
	for _, name := range []string{
		helperAssetMacOS,
		helperDownloadMacOS,
		"闲猫助手-macos.zip",
	} {
		p := filepath.Join(dir, "dist", name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
			return p
		}
	}
	return filepath.Join(dir, "dist", helperDownloadMacOS)
}

func firstExistingFile(paths ...string) string {
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
			return p
		}
	}
	return ""
}

// helperWindowsPackage 返回本机安装包路径、GitHub 资产名、浏览器下载名。
func helperWindowsPackage(dir string) (path, assetName, downloadName string, ok bool) {
	dist := filepath.Join(dir, "dist")
	// 优先 NSIS 安装包，其次便携 zip
	if p := firstExistingFile(
		filepath.Join(dist, helperAssetWindows),
		filepath.Join(dist, helperDownloadWindows),
		filepath.Join(dist, "xianmao-helper-windows-setup-x64.exe"),
	); p != "" {
		return p, helperAssetWindows, helperDownloadWindows, true
	}
	if matches, _ := filepath.Glob(filepath.Join(dist, "xianmao-helper-windows-setup-*.exe")); len(matches) > 0 {
		return matches[0], helperAssetWindows, helperDownloadWindows, true
	}
	if p := firstExistingFile(
		filepath.Join(dist, helperAssetWindowsZip),
		filepath.Join(dist, "xianmao-helper-win-x64.zip"),
	); p != "" {
		return p, helperAssetWindowsZip, "闲猫助手-便携.zip", true
	}
	if matches, _ := filepath.Glob(filepath.Join(dist, "xianmao-helper-win-*.zip")); len(matches) > 0 {
		return matches[0], helperAssetWindowsZip, "闲猫助手-便携.zip", true
	}
	return "", "", "", false
}

func helperLinuxPackage(dir string) (path, assetName, downloadName string, ok bool) {
	dist := filepath.Join(dir, "dist")
	if p := firstExistingFile(
		filepath.Join(dist, helperAssetLinux),
		filepath.Join(dist, helperDownloadLinux),
	); p != "" {
		return p, helperAssetLinux, helperDownloadLinux, true
	}
	if matches, _ := filepath.Glob(filepath.Join(dist, "xianmao-helper-linux-*.AppImage")); len(matches) > 0 {
		return matches[0], helperAssetLinux, helperDownloadLinux, true
	}
	return "", "", "", false
}

func contentTypeForHelper(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".exe"):
		return "application/vnd.microsoft.portable-executable"
	case strings.HasSuffix(lower, ".appimage"):
		return "application/octet-stream"
	case strings.HasSuffix(lower, ".dmg"):
		return "application/x-apple-diskimage"
	default:
		return "application/zip"
	}
}

// contentDispositionAttachment 用 UTF-8 文件名；ASCII fallback 给老浏览器。
func contentDispositionAttachment(downloadName string) string {
	ascii := asciiFilenameFallback(downloadName)
	escaped := url.PathEscape(downloadName)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, escaped)
}

func asciiFilenameFallback(name string) string {
	switch name {
	case helperDownloadMacOS:
		return helperAssetMacOS
	case helperDownloadWindows:
		return helperAssetWindows
	case helperDownloadLinux:
		return helperAssetLinux
	case "闲猫助手-便携.zip":
		return helperAssetWindowsZip
	}
	var b strings.Builder
	for _, r := range name {
		if r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_') {
			b.WriteRune(r)
		}
	}
	if s := b.String(); s != "" && strings.Contains(s, ".") {
		return s
	}
	return "xianmao-helper.bin"
}

func requestIsLoopback(r *http.Request) bool {
	host := r.Header.Get("X-Forwarded-For")
	if host != "" {
		host = strings.TrimSpace(strings.Split(host, ",")[0])
	} else {
		host, _, _ = net.SplitHostPort(r.RemoteAddr)
		if host == "" {
			host = r.RemoteAddr
		}
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func syncHelperDevApp(dir string) error {
	script := filepath.Join(dir, "scripts", "sync-dev-app.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("helper sync script missing")
	}
	cmd := exec.Command("bash", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sync helper app: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureHelperZip(dir string) (string, error) {
	helperAppMu.Lock()
	defer helperAppMu.Unlock()
	if err := syncHelperDevApp(dir); err != nil {
		return "", err
	}
	app := helperAppPath(dir)
	if _, err := os.Stat(app); err != nil {
		return "", fmt.Errorf("helper app missing after sync")
	}
	zipFile := helperZipPath(dir)
	// 已有 zip 且比 app 新则复用
	if zi, zerr := os.Stat(zipFile); zerr == nil {
		if ai, aerr := os.Stat(app); aerr == nil && !zi.ModTime().Before(ai.ModTime()) {
			return zipFile, nil
		}
	}
	_ = os.Remove(zipFile)
	// 打 zip 前暂时摘掉源码符号链接，避免把整个仓库打进去；打完再挂回
	appLink := filepath.Join(app, "Contents", "Resources", "app")
	linkTarget, linkErr := os.Readlink(appLink)
	if linkErr == nil {
		_ = os.Remove(appLink)
		defer func() { _ = os.Symlink(linkTarget, appLink) }()
	}
	cmd := exec.Command("ditto", "-c", "-k", "--keepParent", app, zipFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("zip helper app: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return zipFile, nil
}

func launchHelperDev(dir string) error {
	helperAppMu.Lock()
	defer helperAppMu.Unlock()
	if err := syncHelperDevApp(dir); err != nil {
		return err
	}
	run := filepath.Join(dir, "scripts", "run-dev.sh")
	if _, err := os.Stat(run); err != nil {
		return fmt.Errorf("helper launcher missing")
	}
	cmd := exec.Command("bash", run)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	// 脱离当前请求生命周期
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func helperGitHubRepo() string {
	for _, k := range []string{"UPDATE_GITHUB_REPO", "TM_UPDATE_REPOSITORY", "UPDATE_REPOSITORY"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return "yx-token/token-market"
}

func helperReleaseAssetURL(filename string) string {
	if base := strings.TrimSpace(os.Getenv("TM_HELPER_DOWNLOAD_BASE")); base != "" {
		return strings.TrimRight(base, "/") + "/" + filename
	}
	repo := helperGitHubRepo()
	tag := strings.TrimSpace(os.Getenv("TM_HELPER_RELEASE_TAG"))
	if tag == "" {
		tag = strings.TrimSpace(buildinfo.Version)
	}
	if tag != "" && !strings.EqualFold(tag, "dev") {
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, filename)
	}
	// 嵌入宿主时 market buildinfo 常为 dev：回退到 latest，避免生产无本机包就挂掉
	return fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, filename)
}

func proxyHelperReleaseAsset(w http.ResponseWriter, r *http.Request, assetName, downloadName string) error {
	if downloadName == "" {
		downloadName = assetName
	}
	assetURL := helperReleaseAssetURL(assetName)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, assetURL, nil)
	if err != nil {
		return httpx.BadRequest("HELPER_OS_UNSUPPORTED", "helper package unavailable")
	}
	req.Header.Set("User-Agent", "token-market-helper-download")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return httpx.BadRequest("HELPER_OS_UNSUPPORTED", "helper package unavailable")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return httpx.BadRequest("HELPER_OS_UNSUPPORTED", "helper package unavailable")
	}
	w.Header().Set("Content-Type", contentTypeForHelper(downloadName))
	w.Header().Set("Content-Disposition", contentDispositionAttachment(downloadName))
	w.Header().Set("Cache-Control", "no-store")
	if cl := res.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	// 显式 200：嵌入宿主若经 Gin NoRoute 可能已预置 404，不 WriteHeader 会把 zip 标成失败。
	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, res.Body)
	return err
}

// GET /api/one-click/helper/download/{os}
func (s *Server) downloadOneClickHelper(w http.ResponseWriter, r *http.Request) error {
	osName := strings.ToLower(strings.TrimSpace(r.PathValue("os")))
	if osName == "" {
		osName = "macos"
	}
	dir := helperDesktopDir()
	var (
		file         string
		assetName    string
		downloadName string
		err          error
	)
	switch osName {
	case "macos", "darwin":
		assetName = helperAssetMacOS
		downloadName = helperDownloadMacOS
		if dir != "" {
			if p := firstExistingFile(
				filepath.Join(dir, "dist", helperAssetMacOS),
				filepath.Join(dir, "dist", helperDownloadMacOS),
				filepath.Join(dir, "dist", "闲猫助手-macos.zip"),
			); p != "" {
				file = p
			} else if requestIsLoopback(r) || strings.TrimSpace(os.Getenv("TM_HELPER_BUILD")) == "1" {
				file, err = ensureHelperZip(dir)
				if err != nil {
					return httpx.BadRequest("HELPER_BUILD_FAILED", "helper package unavailable")
				}
			}
		}
	case "windows", "win", "win32":
		assetName = helperAssetWindows
		downloadName = helperDownloadWindows
		if dir != "" {
			var ok bool
			file, assetName, downloadName, ok = helperWindowsPackage(dir)
			if !ok {
				assetName = helperAssetWindows
				downloadName = helperDownloadWindows
			}
		}
	case "linux":
		assetName = helperAssetLinux
		downloadName = helperDownloadLinux
		if dir != "" {
			var ok bool
			file, assetName, downloadName, ok = helperLinuxPackage(dir)
			if !ok {
				assetName = helperAssetLinux
				downloadName = helperDownloadLinux
			}
		}
	default:
		return httpx.BadRequest("HELPER_OS_UNSUPPORTED", "os must be macos, windows, or linux")
	}
	if file != "" {
		w.Header().Set("Content-Type", contentTypeForHelper(downloadName))
		w.Header().Set("Content-Disposition", contentDispositionAttachment(downloadName))
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, file)
		return nil
	}
	// 生产常见：MARKET_EMBED 无本机 dist。回源 GitHub Release（v0.17.61 起已上传安装包）。
	return proxyHelperReleaseAsset(w, r, assetName, downloadName)
}

func probeHelperLocal(timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	res, err := client.Get("http://127.0.0.1:39217/health")
	if err != nil {
		return false
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return false
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	var parsed struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return false
	}
	return parsed.OK
}

// GET /api/one-click/helper/status — 由站点代探本机助手（避免浏览器直连 39217 被 Local Network Access 拦住）
// 非本机请求不得把「跑站点的那台机器」上的助手当成用户电脑上的助手（局域网 Windows 测 Mac 站点时会误跳过下载）。
func (s *Server) statusOneClickHelper(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	if !requestIsLoopback(r) {
		return ok(w, map[string]any{"ok": true, "online": false, "scope": "remote"})
	}
	online := probeHelperLocal(900 * time.Millisecond)
	return ok(w, map[string]any{"ok": true, "online": online, "scope": "local"})
}

// POST /api/one-click/helper/launch — 本机自动启动开发版助手（仅 loopback）
func (s *Server) launchOneClickHelper(w http.ResponseWriter, r *http.Request) error {
	if !requestIsLoopback(r) {
		return httpx.Forbidden("HELPER_LAUNCH_FORBIDDEN", "helper launch only allowed on local machine")
	}
	dir := helperDesktopDir()
	if dir == "" {
		return httpx.BadRequest("HELPER_UNAVAILABLE", "helper package unavailable")
	}
	if err := launchHelperDev(dir); err != nil {
		return httpx.BadRequest("HELPER_LAUNCH_FAILED", "helper launch failed")
	}
	// 给进程一点启动时间
	time.Sleep(800 * time.Millisecond)
	online := probeHelperLocal(1200 * time.Millisecond)
	w.Header().Set("Cache-Control", "no-store")
	return ok(w, map[string]any{"ok": true, "launched": true, "online": online})
}
