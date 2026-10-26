//go:build unit

package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release        *GitHubRelease
	recentReleases []*GitHubRelease
	recentErr      error
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(context.Context, string) (*GitHubRelease, error) {
	return s.release, nil
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.1.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.1.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.144", versions[1].Version)
	require.Equal(t, "0.1.143", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.145", versions[1].Version)
	require.Equal(t, "0.1.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.148"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148"},
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
		{TagName: "v0.1.144"},
		{TagName: "v0.1.143"},
		{TagName: "v0.1.142"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.1.147",  // current version
		"v0.1.147", // current version with prefix
		"0.1.148",  // newer than current
		"0.1.142",  // older than the 3 most recent
		"9.9.9",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}

type marketWebArchiveEntry struct {
	name string
	data string
	dir  bool
}

func writeMarketWebArchive(t *testing.T, entries ...marketWebArchiveEntry) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "release.tar.gz")
	f, err := os.Create(archivePath)
	require.NoError(t, err)
	gz := gzip.NewWriter(f)
	tr := tar.NewWriter(gz)
	for _, entry := range entries {
		hdr := &tar.Header{Name: entry.name, Mode: 0o644}
		if entry.dir {
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0o755
		} else {
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(entry.data))
		}
		require.NoError(t, tr.WriteHeader(hdr))
		if !entry.dir {
			_, err := tr.Write([]byte(entry.data))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tr.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, f.Close())
	return archivePath
}

func writeMarketWebFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func extractMarketWebForTest(t *testing.T, archivePath, destDir string) error {
	t.Helper()
	var svc UpdateService
	staging, err := svc.prepareMarketWeb(archivePath, destDir)
	if err != nil || staging == "" {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	return svc.mergeMarketWeb(staging, destDir)
}

func TestExtractMarketWebMergesInPlaceAndCleansOldFiles(t *testing.T) {
	destDir := filepath.Join(t.TempDir(), "market-web")
	writeMarketWebFile(t, destDir, "index.html", "old index")
	writeMarketWebFile(t, destDir, "assets/app-old.js", "old hash")
	writeMarketWebFile(t, destDir, "stale.css", "remove me")

	archivePath := writeMarketWebArchive(t,
		marketWebArchiveEntry{name: "market-web/index.html", data: "new index"},
		marketWebArchiveEntry{name: "market-web/assets/app-new.js", data: "new hash"},
		marketWebArchiveEntry{name: "market-web/assets", dir: true},
	)
	require.NoError(t, extractMarketWebForTest(t, archivePath, destDir))

	content, err := os.ReadFile(filepath.Join(destDir, "index.html"))
	require.NoError(t, err)
	require.Equal(t, "new index", string(content))
	content, err = os.ReadFile(filepath.Join(destDir, "assets/app-new.js"))
	require.NoError(t, err)
	require.Equal(t, "new hash", string(content))
	_, err = os.Stat(filepath.Join(destDir, "assets/app-old.js"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(destDir, "stale.css"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(destDir + ".bak")
	require.ErrorIs(t, err, os.ErrNotExist)

	err = filepath.WalkDir(destDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		require.False(t, strings.HasPrefix(d.Name(), ".market-web-tmp-"), "temporary file left behind: %s", path)
		return nil
	})
	require.NoError(t, err)
}

func TestExtractMarketWebRejectsTraversalAndAbsolutePaths(t *testing.T) {
	for _, name := range []string{"market-web/../evil.js", "market-web//absolute.js"} {
		t.Run(name, func(t *testing.T) {
			destDir := filepath.Join(t.TempDir(), "market-web")
			writeMarketWebFile(t, destDir, "existing.js", "keep")
			archivePath := writeMarketWebArchive(t, marketWebArchiveEntry{name: name, data: "bad"})
			err := extractMarketWebForTest(t, archivePath, destDir)
			require.Error(t, err)
			require.Contains(t, err.Error(), "market-web")
			content, readErr := os.ReadFile(filepath.Join(destDir, "existing.js"))
			require.NoError(t, readErr)
			require.Equal(t, "keep", string(content))
			_, statErr := os.Stat(filepath.Join(filepath.Dir(destDir), "evil.js"))
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func TestReplaceBinaryWithMergeRestoresOldBinary(t *testing.T) {
	root := t.TempDir()
	exePath := filepath.Join(root, "sub2api")
	newPath := filepath.Join(root, "sub2api.new")
	backupPath := exePath + ".backup"
	require.NoError(t, os.WriteFile(exePath, []byte("old binary"), 0o755))
	require.NoError(t, os.WriteFile(newPath, []byte("new binary"), 0o755))

	err := replaceBinaryWithMerge(exePath, newPath, backupPath, func() error {
		return errors.New("simulated market-web merge failure")
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "binary restored")
	content, readErr := os.ReadFile(exePath)
	require.NoError(t, readErr)
	require.Equal(t, "old binary", string(content))
	_, statErr := os.Stat(backupPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}
