package service

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/yx-token/token-market/internal/pkg/errors"
)

var (
	ErrNoUpdateAvailable         = infraerrors.Conflict("ALREADY_UP_TO_DATE", "no update available; current version is latest")
	ErrRollbackVersionNotAllowed = infraerrors.BadRequest("ROLLBACK_VERSION_NOT_ALLOWED", "version is not in the allowed rollback list")
)

const (
	updateCacheKey = "update_check_cache"
	updateCacheTTL = 1200 // 20 minutes
	// 默认本仓；社区上游 Wei-Shaw/sub2api 会装错二进制。可用 UPDATE_GITHUB_REPO 覆盖。
	defaultUpdateGitHubRepo = "yx-token/token-market"

	// Security: allowed download domains for updates
	allowedDownloadHost = "github.com"
	allowedAssetHost    = "objects.githubusercontent.com"

	// Security: max download size (500MB)
	maxDownloadSize = 500 * 1024 * 1024

	// Rollback: expose at most the 3 most recent versions older than current
	maxRollbackVersions = 3
	// Fetch a few extra releases so filtering (current/newer/prerelease) still leaves enough candidates
	rollbackFetchPageSize = 15
)

func updateGitHubRepo() string {
	if v := strings.TrimSpace(os.Getenv("UPDATE_GITHUB_REPO")); v != "" {
		return v
	}
	return defaultUpdateGitHubRepo
}

// UpdateCache defines cache operations for update service
type UpdateCache interface {
	GetUpdateInfo(ctx context.Context) (string, error)
	SetUpdateInfo(ctx context.Context, data string, ttl time.Duration) error
}

// GitHubReleaseClient 获取 GitHub release 信息的接口
type GitHubReleaseClient interface {
	FetchLatestRelease(ctx context.Context, repo string) (*GitHubRelease, error)
	FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*GitHubRelease, error)
	DownloadFile(ctx context.Context, url, dest string, maxSize int64) error
	FetchChecksumFile(ctx context.Context, url string) ([]byte, error)
}

// UpdateService handles software updates
type UpdateService struct {
	cache          UpdateCache
	githubClient   GitHubReleaseClient
	currentVersion string
	buildType      string // "source" for manual builds, "release" for CI builds
}

// NewUpdateService creates a new UpdateService
func NewUpdateService(cache UpdateCache, githubClient GitHubReleaseClient, version, buildType string) *UpdateService {
	return &UpdateService{
		cache:          cache,
		githubClient:   githubClient,
		currentVersion: version,
		buildType:      buildType,
	}
}

// UpdateInfo contains update information
type UpdateInfo struct {
	CurrentVersion string       `json:"current_version"`
	LatestVersion  string       `json:"latest_version"`
	HasUpdate      bool         `json:"has_update"`
	ReleaseInfo    *ReleaseInfo `json:"release_info,omitempty"`
	Cached         bool         `json:"cached"`
	Warning        string       `json:"warning,omitempty"`
	BuildType      string       `json:"build_type"` // "source" or "release"
}

// ReleaseInfo contains GitHub release details
type ReleaseInfo struct {
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Assets      []Asset `json:"assets,omitempty"`
}

// Asset represents a release asset
type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
}

// GitHubRelease represents GitHub API response
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	PublishedAt string        `json:"published_at"`
	HTMLURL     string        `json:"html_url"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []GitHubAsset `json:"assets"`
}

// RollbackVersion describes a release version the system can roll back to
type RollbackVersion struct {
	Version     string `json:"version"` // without "v" prefix, e.g. "0.1.146"
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// CheckUpdate checks for available updates
func (s *UpdateService) CheckUpdate(ctx context.Context, force bool) (*UpdateInfo, error) {
	// Try cache first
	if !force {
		if cached, err := s.getFromCache(ctx); err == nil && cached != nil {
			return cached, nil
		}
	}

	// Fetch from GitHub
	info, err := s.fetchLatestRelease(ctx)
	if err != nil {
		// Return cached on error
		if cached, cacheErr := s.getFromCache(ctx); cacheErr == nil && cached != nil {
			cached.Warning = "Using cached data: " + err.Error()
			return cached, nil
		}
		return &UpdateInfo{
			CurrentVersion: s.currentVersion,
			LatestVersion:  s.currentVersion,
			HasUpdate:      false,
			Warning:        err.Error(),
			BuildType:      s.buildType,
		}, nil
	}

	// Cache result
	s.saveToCache(ctx, info)
	return info, nil
}

// PerformUpdate downloads and applies the update
// Uses atomic file replacement pattern for safe in-place updates
func (s *UpdateService) PerformUpdate(ctx context.Context) error {
	info, err := s.CheckUpdate(ctx, true)
	if err != nil {
		return err
	}

	if !info.HasUpdate {
		return ErrNoUpdateAvailable
	}

	return s.applyReleaseAssets(ctx, info.ReleaseInfo.Assets)
}

// applyReleaseAssets downloads the platform archive from the given release assets,
// verifies its checksum, and atomically swaps the running binary.
// Shared by PerformUpdate (latest) and RollbackToVersion (specific older version).
func (s *UpdateService) applyReleaseAssets(ctx context.Context, releaseAssets []Asset) error {
	// Find matching archive and checksum for current platform.
	// 同 Release 还有 token-market_linux_*.tar.gz，必须优先 sub2api_ 宿主包。
	platform := s.getArchiveName()
	var downloadURL string
	var checksumURL string
	var fallbackURL string

	for _, asset := range releaseAssets {
		name := asset.Name
		if name == "checksums.txt" {
			checksumURL = asset.DownloadURL
			continue
		}
		if strings.HasSuffix(name, ".txt") || !strings.Contains(name, platform) {
			continue
		}
		if strings.HasPrefix(name, "sub2api_") {
			downloadURL = asset.DownloadURL
			continue
		}
		if fallbackURL == "" && !strings.HasPrefix(name, "token-market_") {
			fallbackURL = asset.DownloadURL
		}
	}
	if downloadURL == "" {
		downloadURL = fallbackURL
	}

	if downloadURL == "" {
		return fmt.Errorf("no compatible release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// SECURITY: Validate download URL is from trusted domain
	if err := validateDownloadURL(downloadURL); err != nil {
		return fmt.Errorf("invalid download URL: %w", err)
	}
	if checksumURL != "" {
		if err := validateDownloadURL(checksumURL); err != nil {
			return fmt.Errorf("invalid checksum URL: %w", err)
		}
	}

	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	exeDir := filepath.Dir(exePath)

	// Create temp directory in the SAME directory as executable
	// This ensures os.Rename is atomic (same filesystem)
	tempDir, err := os.MkdirTemp(exeDir, ".sub2api-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Download archive
	archivePath := filepath.Join(tempDir, filepath.Base(downloadURL))
	if err := s.downloadFile(ctx, downloadURL, archivePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Verify checksum if available
	if checksumURL != "" {
		if err := s.verifyChecksum(ctx, archivePath, checksumURL); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Extract binary from archive
	newBinaryPath := filepath.Join(tempDir, "sub2api")
	if err := s.extractBinary(archivePath, newBinaryPath); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Set executable permission before replacement
	if err := os.Chmod(newBinaryPath, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Prepare market-web before replacing the binary. This validates the complete
	// static asset tree while the old executable is still in place, so a bad
	// archive cannot leave a half-applied update.
	var marketWebStaging string
	var marketWebDir string
	if webDir, ok := trustedMarketWebDir(os.Getenv("TM_HTTP_WEB_DIR")); ok {
		marketWebDir = webDir
		marketWebStaging, err = s.prepareMarketWeb(archivePath, webDir)
		if err != nil {
			return fmt.Errorf("market-web preparation failed: %w", err)
		}
		if marketWebStaging != "" {
			defer func() { _ = os.RemoveAll(marketWebStaging) }()
		}
	}

	// Atomic replacement using rename pattern:
	// 1. Rename current -> backup (atomic on Unix)
	// 2. Rename new -> current (atomic on Unix, same filesystem)
	// If step 2 or the market-web merge fails, restore the backup.
	backupPath := exePath + ".backup"

	// 单进程：若包内有 market-web/ 且配置了 TM_HTTP_WEB_DIR，同步 C 端静态。
	// mergeMarketWeb restores the old tree on its own failure; restore the
	// executable as well so an update is all-or-nothing.
	var merge func() error
	if marketWebStaging != "" {
		merge = func() error { return s.mergeMarketWeb(marketWebStaging, marketWebDir) }
	}
	if err := replaceBinaryWithMerge(exePath, newBinaryPath, backupPath, merge); err != nil {
		return err
	}

	// Success - backup file is kept for rollback capability
	// It will be cleaned up on next successful update
	return nil
}

// Rollback restores the previous version
func (s *UpdateService) Rollback() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	backupFile := exePath + ".backup"
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		return fmt.Errorf("no backup found")
	}

	// Replace current with backup
	if err := os.Rename(backupFile, exePath); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	return nil
}

// ListRollbackVersions returns up to maxRollbackVersions release versions that are
// strictly older than the current version (the current version itself is excluded),
// newest first. Draft and prerelease entries are skipped.
func (s *UpdateService) ListRollbackVersions(ctx context.Context) ([]RollbackVersion, error) {
	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]RollbackVersion, 0, len(releases))
	for _, r := range releases {
		versions = append(versions, RollbackVersion{
			Version:     strings.TrimPrefix(r.TagName, "v"),
			PublishedAt: r.PublishedAt,
			HTMLURL:     r.HTMLURL,
		})
	}
	return versions, nil
}

// RollbackToVersion downloads and installs a specific older version.
// The target must be one of the versions returned by ListRollbackVersions;
// anything else (including the current version) is rejected.
func (s *UpdateService) RollbackToVersion(ctx context.Context, version string) error {
	target := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if target == "" {
		return ErrRollbackVersionNotAllowed
	}

	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return err
	}

	var match *GitHubRelease
	for _, r := range releases {
		if strings.TrimPrefix(r.TagName, "v") == target {
			match = r
			break
		}
	}
	if match == nil {
		return ErrRollbackVersionNotAllowed
	}

	assets := make([]Asset, len(match.Assets))
	for i, a := range match.Assets {
		assets[i] = Asset{
			Name:        a.Name,
			DownloadURL: a.BrowserDownloadURL,
			Size:        a.Size,
		}
	}

	return s.applyReleaseAssets(ctx, assets)
}

// fetchRollbackCandidates fetches recent releases and keeps the newest
// maxRollbackVersions entries strictly older than the current version.
func (s *UpdateService) fetchRollbackCandidates(ctx context.Context) ([]*GitHubRelease, error) {
	releases, err := s.githubClient.FetchRecentReleases(ctx, updateGitHubRepo(), rollbackFetchPageSize)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(releases))
	candidates := make([]*GitHubRelease, 0, maxRollbackVersions)
	for _, r := range releases {
		if r == nil || r.Draft || r.Prerelease {
			continue
		}
		v := strings.TrimPrefix(r.TagName, "v")
		if v == "" || seen[v] {
			continue
		}
		// Only versions strictly older than current (also excludes current itself)
		if compareVersions(v, s.currentVersion) >= 0 {
			continue
		}
		seen[v] = true
		candidates = append(candidates, r)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return compareVersions(
			strings.TrimPrefix(candidates[i].TagName, "v"),
			strings.TrimPrefix(candidates[j].TagName, "v"),
		) > 0
	})

	if len(candidates) > maxRollbackVersions {
		candidates = candidates[:maxRollbackVersions]
	}
	return candidates, nil
}

func (s *UpdateService) fetchLatestRelease(ctx context.Context) (*UpdateInfo, error) {
	release, err := s.githubClient.FetchLatestRelease(ctx, updateGitHubRepo())
	if err != nil {
		return nil, err
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")

	assets := make([]Asset, len(release.Assets))
	for i, a := range release.Assets {
		assets[i] = Asset{
			Name:        a.Name,
			DownloadURL: a.BrowserDownloadURL,
			Size:        a.Size,
		}
	}

	return &UpdateInfo{
		CurrentVersion: s.currentVersion,
		LatestVersion:  latestVersion,
		HasUpdate:      compareVersions(s.currentVersion, latestVersion) < 0,
		ReleaseInfo: &ReleaseInfo{
			Name:        release.Name,
			Body:        release.Body,
			PublishedAt: release.PublishedAt,
			HTMLURL:     release.HTMLURL,
			Assets:      assets,
		},
		Cached:    false,
		BuildType: s.buildType,
	}, nil
}

func (s *UpdateService) downloadFile(ctx context.Context, downloadURL, dest string) error {
	return s.githubClient.DownloadFile(ctx, downloadURL, dest, maxDownloadSize)
}

func (s *UpdateService) getArchiveName() string {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("%s_%s", osName, arch)
}

// validateDownloadURL checks if the URL is from an allowed domain
// SECURITY: This prevents SSRF and ensures downloads only come from trusted GitHub domains
func validateDownloadURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Must be HTTPS
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTPS URLs are allowed")
	}

	// Check against allowed hosts
	host := parsedURL.Host
	// GitHub release URLs can be from github.com or objects.githubusercontent.com
	if host != allowedDownloadHost &&
		!strings.HasSuffix(host, "."+allowedDownloadHost) &&
		host != allowedAssetHost &&
		!strings.HasSuffix(host, "."+allowedAssetHost) {
		return fmt.Errorf("download from untrusted host: %s", host)
	}

	return nil
}

func (s *UpdateService) verifyChecksum(ctx context.Context, filePath, checksumURL string) error {
	// Download checksums file
	checksumData, err := s.githubClient.FetchChecksumFile(ctx, checksumURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}

	// Calculate file hash
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	// Find expected hash in checksums file
	fileName := filepath.Base(filePath)
	scanner := bufio.NewScanner(strings.NewReader(string(checksumData)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == fileName {
			if parts[0] == actualHash {
				return nil
			}
			return fmt.Errorf("checksum mismatch: expected %s, got %s", parts[0], actualHash)
		}
	}

	return fmt.Errorf("checksum not found for %s", fileName)
}

func (s *UpdateService) extractBinary(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var reader io.Reader = f

	// Handle gzip compression
	if strings.HasSuffix(archivePath, ".gz") || strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer func() { _ = gzr.Close() }()
		reader = gzr
	}

	// Handle tar archive
	if strings.Contains(archivePath, ".tar") {
		tr := tar.NewReader(reader)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			// SECURITY: Prevent Zip Slip / Path Traversal attack
			// Only allow files with safe base names, no directory traversal
			baseName := filepath.Base(hdr.Name)

			// Check for path traversal attempts
			if strings.Contains(hdr.Name, "..") {
				return fmt.Errorf("path traversal attempt detected: %s", hdr.Name)
			}

			// Validate the entry is a regular file
			if hdr.Typeflag != tar.TypeReg {
				continue // Skip directories and special files
			}

			// Only extract the specific binary we need
			if baseName == "sub2api" || baseName == "sub2api.exe" {
				// Additional security: limit file size (max 500MB)
				const maxBinarySize = 500 * 1024 * 1024
				if hdr.Size > maxBinarySize {
					return fmt.Errorf("binary too large: %d bytes (max %d)", hdr.Size, maxBinarySize)
				}

				out, err := os.Create(destPath)
				if err != nil {
					return err
				}

				// Use LimitReader to prevent decompression bombs
				limited := io.LimitReader(tr, maxBinarySize)
				if _, err := io.Copy(out, limited); err != nil {
					_ = out.Close()
					return err
				}
				if err := out.Close(); err != nil {
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("binary not found in archive")
	}

	// Direct copy for non-tar files (with size limit)
	const maxBinarySize = 500 * 1024 * 1024
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}

	limited := io.LimitReader(reader, maxBinarySize)
	if _, err := io.Copy(out, limited); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// trustedMarketWebDir 只允许部署约定目录，返回字面量以切断 Getenv 污点（gosec G703）。
func trustedMarketWebDir(raw string) (string, bool) {
	switch filepath.Clean(strings.TrimSpace(raw)) {
	case "/app/market-web":
		return "/app/market-web", true
	default:
		return "", false
	}
}

const marketWebMaxFileSize = 32 << 20 // 32MiB per asset

// prepareMarketWeb 从宿主更新包中解压 market-web/ 到同一文件系统的 staging
// 目录。它只读取和校验归档，不触碰 destDir；调用方负责在完成后删除 staging。
// 包内无该目录时视为旧包，返回空路径。
func (s *UpdateService) prepareMarketWeb(archivePath, destDir string) (stagingPath string, err error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	var reader io.Reader = f
	if strings.HasSuffix(archivePath, ".gz") || strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		defer func() { _ = gzr.Close() }()
		reader = gzr
	}
	if !strings.Contains(archivePath, ".tar") {
		return "", nil
	}

	staging, err := os.MkdirTemp(filepath.Dir(destDir), ".market-web-update-*")
	if err != nil {
		return "", err
	}
	keepStaging := false
	defer func() {
		if !keepStaging {
			_ = os.RemoveAll(staging)
		}
	}()

	found := false
	stagingClean := filepath.Clean(staging)
	safeUnderStaging := func(rel string) (string, error) {
		rel = filepath.Clean(rel)
		if rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("invalid market-web path: %q", rel)
		}
		// 禁止绝对路径（Clean 后仍可能以分隔符开头）
		if filepath.IsAbs(rel) {
			return "", fmt.Errorf("absolute market-web path rejected: %q", rel)
		}
		target := filepath.Join(stagingClean, rel)
		// 确认落在 staging 目录内，堵住 path traversal
		prefix := stagingClean + string(os.PathSeparator)
		if target != stagingClean && !strings.HasPrefix(target, prefix) {
			return "", fmt.Errorf("path escapes staging: %q", rel)
		}
		return target, nil
	}
	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if filepath.IsAbs(name) {
			return "", fmt.Errorf("absolute market-web path rejected: %q", hdr.Name)
		}
		if (name == "market-web" || name == "market-web/") && hdr.Typeflag == tar.TypeDir {
			// An explicitly present but empty market-web directory is still a
			// valid bundle and should clear stale files from the destination.
			found = true
			continue
		}
		if !strings.HasPrefix(name, "market-web/") {
			continue
		}
		rel := strings.TrimPrefix(name, "market-web/")
		if rel == "" {
			continue
		}
		target, err := safeUnderStaging(rel)
		if err != nil {
			return "", err
		}
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
			found = true
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if hdr.Size > marketWebMaxFileSize {
			return "", fmt.Errorf("market-web file too large: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", err
		}
		out, err := os.Create(target)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, marketWebMaxFileSize)); err != nil {
			_ = out.Close()
			return "", err
		}
		if err := out.Close(); err != nil {
			return "", err
		}
		found = true
	}
	if !found {
		return "", nil
	}
	keepStaging = true
	return staging, nil
}

// restoreBinaryBackup restores the old executable after a later asset merge
// fails. Removing the new path first also works when the target is a running
// Linux executable; the old file is then renamed into its original location.
func restoreBinaryBackup(exePath, backupPath string) error {
	if err := os.Remove(exePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove new binary: %w", err)
	}
	if err := os.Rename(backupPath, exePath); err != nil {
		return fmt.Errorf("restore backup binary: %w", err)
	}
	return nil
}

// extractMarketWeb is kept as a small compatibility wrapper for callers that
// need to apply a complete archive outside the update transaction.
//
//nolint:unused // the update transaction uses the split prepare/merge stages.
func (s *UpdateService) extractMarketWeb(archivePath, destDir string) error {
	staging, err := s.prepareMarketWeb(archivePath, destDir)
	if err != nil || staging == "" {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	return s.mergeMarketWeb(staging, destDir)
}

// replaceBinaryWithMerge performs the executable swap and an optional asset
// merge as one recoverable operation. The callback runs only after the new
// executable is in place; any callback error restores the old executable.
func replaceBinaryWithMerge(exePath, newBinaryPath, backupPath string, merge func() error) error {
	_ = os.Remove(backupPath)
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		if restoreErr := os.Rename(backupPath, exePath); restoreErr != nil {
			return fmt.Errorf("replace failed and restore failed: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace failed (restored backup): %w", err)
	}
	if merge == nil {
		return nil
	}
	if err := merge(); err != nil {
		if restoreErr := restoreBinaryBackup(exePath, backupPath); restoreErr != nil {
			return fmt.Errorf("market-web merge failed and binary restore failed: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("market-web merge failed; binary restored: %w", err)
	}
	return nil
}

type marketWebManifestEntry struct {
	directory bool
}

func collectMarketWebManifest(staging string) (map[string]marketWebManifestEntry, []string, []string, error) {
	manifest := make(map[string]marketWebManifestEntry)
	var dirs, files []string
	err := filepath.WalkDir(staging, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == staging {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("market-web symlink rejected: %s", path)
		}
		rel, err := filepath.Rel(staging, path)
		if err != nil {
			return err
		}
		rel = filepath.Clean(rel)
		if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("invalid market-web path: %q", rel)
		}
		if d.IsDir() {
			manifest[rel] = marketWebManifestEntry{directory: true}
			dirs = append(dirs, rel)
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("unsupported market-web entry: %s", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > marketWebMaxFileSize {
			return fmt.Errorf("market-web file too large: %s", rel)
		}
		manifest[rel] = marketWebManifestEntry{}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	sort.Slice(dirs, func(i, j int) bool {
		depthI := strings.Count(dirs[i], string(os.PathSeparator))
		depthJ := strings.Count(dirs[j], string(os.PathSeparator))
		if depthI != depthJ {
			return depthI < depthJ
		}
		return dirs[i] < dirs[j]
	})
	sort.Strings(files)
	return manifest, dirs, files, nil
}

// copyMarketWebTree copies a directory without following symlinks. The copy
// is deliberately used for the backup so the destination directory itself is
// never renamed across an overlayfs layer boundary.
func copyMarketWebTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("market-web source is not a directory: %s", src)
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	err = filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == src {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("market-web symlink rejected: %s", rel)
		}
		entryInfo, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			if err := os.MkdirAll(target, entryInfo.Mode().Perm()); err != nil {
				return err
			}
			return os.Chmod(target, entryInfo.Mode().Perm())
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("unsupported market-web entry: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, entryInfo.Mode().Perm())
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeOutErr := out.Close()
		closeInErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		if closeInErr != nil {
			return closeInErr
		}
		return os.Chmod(target, entryInfo.Mode().Perm())
	})
	return err
}

func restoreMarketWebTree(destDir, backup string, existed bool) error {
	if err := os.RemoveAll(destDir); err != nil {
		return err
	}
	if !existed {
		return nil
	}
	return copyMarketWebTree(backup, destDir)
}

func reconcileMarketWebTree(staging, destDir string, manifest map[string]marketWebManifestEntry, dirs, files []string) error {
	if err := filepath.WalkDir(destDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == destDir {
			return nil
		}
		rel, err := filepath.Rel(destDir, path)
		if err != nil {
			return err
		}
		entry, wanted := manifest[filepath.Clean(rel)]
		matches := wanted && ((entry.directory && d.IsDir()) || (!entry.directory && d.Type().IsRegular()))
		if matches {
			return nil
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}); err != nil {
		return err
	}

	for _, rel := range dirs {
		target := filepath.Join(destDir, rel)
		if info, err := os.Lstat(target); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				if err := os.RemoveAll(target); err != nil {
					return err
				}
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(target, 0755); err != nil {
			return err
		}
	}

	for _, rel := range files {
		source := filepath.Join(staging, rel)
		target := filepath.Join(destDir, rel)
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		if info.Size() > marketWebMaxFileSize {
			return fmt.Errorf("market-web file too large: %s", rel)
		}
		if existing, err := os.Lstat(target); err == nil && (existing.IsDir() || existing.Mode()&os.ModeSymlink != 0) {
			if err := os.RemoveAll(target); err != nil {
				return err
			}
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		in, err := os.Open(source)
		if err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(target), ".market-web-tmp-*")
		if err != nil {
			_ = in.Close()
			return err
		}
		tmpName := tmp.Name()
		copyErr := func() error {
			defer func() { _ = os.Remove(tmpName) }()
			if _, err := io.Copy(tmp, io.LimitReader(in, marketWebMaxFileSize)); err != nil {
				return err
			}
			if err := tmp.Chmod(info.Mode().Perm()); err != nil {
				return err
			}
			if err := tmp.Close(); err != nil {
				return err
			}
			return os.Rename(tmpName, target)
		}()
		closeInErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInErr != nil {
			return closeInErr
		}
	}
	return nil
}

// mergeMarketWeb atomically replaces individual files in destDir while
// preserving the directory inode. This works when destDir originated in an
// overlayfs lower layer, where renaming the directory itself returns EXDEV.
func (s *UpdateService) mergeMarketWeb(staging, destDir string) (err error) {
	stagingInfo, err := os.Lstat(staging)
	if err != nil {
		return err
	}
	if stagingInfo.Mode()&os.ModeSymlink != 0 || !stagingInfo.IsDir() {
		return fmt.Errorf("market-web staging is not a directory")
	}

	destInfo, destErr := os.Lstat(destDir)
	destExists := destErr == nil
	if destErr != nil && !os.IsNotExist(destErr) {
		return destErr
	}
	if destExists && (destInfo.Mode()&os.ModeSymlink != 0 || !destInfo.IsDir()) {
		return fmt.Errorf("market-web destination is not a directory: %s", destDir)
	}

	manifest, dirs, files, err := collectMarketWebManifest(staging)
	if err != nil {
		return err
	}
	backup := destDir + ".bak"
	_ = os.RemoveAll(backup)
	if destExists {
		if err := copyMarketWebTree(destDir, backup); err != nil {
			_ = os.RemoveAll(backup)
			return fmt.Errorf("backup market-web failed: %w", err)
		}
	}
	defer func() {
		if err == nil {
			return
		}
		restoreErr := restoreMarketWebTree(destDir, backup, destExists)
		if restoreErr != nil {
			err = fmt.Errorf("%w (market-web restore failed: %v)", err, restoreErr)
		}
		_ = os.RemoveAll(backup)
	}()

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	if err := reconcileMarketWebTree(staging, destDir, manifest, dirs, files); err != nil {
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
}

func (s *UpdateService) getFromCache(ctx context.Context) (*UpdateInfo, error) {
	data, err := s.cache.GetUpdateInfo(ctx)
	if err != nil {
		return nil, err
	}

	var cached struct {
		Latest      string       `json:"latest"`
		ReleaseInfo *ReleaseInfo `json:"release_info"`
		Timestamp   int64        `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(data), &cached); err != nil {
		return nil, err
	}

	if time.Now().Unix()-cached.Timestamp > updateCacheTTL {
		return nil, fmt.Errorf("cache expired")
	}

	return &UpdateInfo{
		CurrentVersion: s.currentVersion,
		LatestVersion:  cached.Latest,
		HasUpdate:      compareVersions(s.currentVersion, cached.Latest) < 0,
		ReleaseInfo:    cached.ReleaseInfo,
		Cached:         true,
		BuildType:      s.buildType,
	}, nil
}

func (s *UpdateService) saveToCache(ctx context.Context, info *UpdateInfo) {
	cacheData := struct {
		Latest      string       `json:"latest"`
		ReleaseInfo *ReleaseInfo `json:"release_info"`
		Timestamp   int64        `json:"timestamp"`
	}{
		Latest:      info.LatestVersion,
		ReleaseInfo: info.ReleaseInfo,
		Timestamp:   time.Now().Unix(),
	}

	data, _ := json.Marshal(cacheData)
	_ = s.cache.SetUpdateInfo(ctx, string(data), time.Duration(updateCacheTTL)*time.Second)
}

// compareVersions compares two semantic versions
func compareVersions(current, latest string) int {
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)

	for i := 0; i < 3; i++ {
		if currentParts[i] < latestParts[i] {
			return -1
		}
		if currentParts[i] > latestParts[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexByte(v, '-'); idx != -1 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	result := [3]int{0, 0, 0}
	for i := 0; i < len(parts) && i < 3; i++ {
		if parsed, err := strconv.Atoi(parts[i]); err == nil {
			result[i] = parsed
		}
	}
	return result
}
