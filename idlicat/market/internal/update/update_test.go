package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0755); err != nil {
		t.Fatal(err)
	}
}

func manifest(version string) []byte {
	raw, _ := json.Marshal(Manifest{Schema: 1, Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH})
	return raw
}

func bundle(t *testing.T, version string, extra *tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	files := map[string][]byte{"manifest.json": manifest(version), "web/index.html": []byte("frontend " + version)}
	for _, name := range []string{"api", "worker", "edge", "s2a-compat"} {
		files["bin/"+name] = []byte("binary " + version)
	}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if extra != nil {
		if err := tw.WriteHeader(extra); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestArchiveValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header *tar.Header
	}{
		{"valid", nil},
		{"traversal", &tar.Header{Name: "web/../../outside", Typeflag: tar.TypeReg}},
		{"absolute", &tar.Header{Name: "/tmp/outside", Typeflag: tar.TypeReg}},
		{"symlink", &tar.Header{Name: "web/link", Typeflag: tar.TypeSymlink, Linkname: "/tmp"}},
		{"hardlink", &tar.Header{Name: "web/link", Typeflag: tar.TypeLink, Linkname: "bin/api"}},
		{"duplicate", &tar.Header{Name: "bin/api", Typeflag: tar.TypeReg}},
		{"unexpected", &tar.Header{Name: "bin/market-runner", Typeflag: tar.TypeReg}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "package.tar.gz")
			write(t, archive, bundle(t, "v1.1.0", tc.header))
			err := extractRelease(archive, t.TempDir(), "v1.1.0")
			if (err != nil) != (tc.header != nil) {
				t.Fatalf("extract error: %v", err)
			}
			if tc.header == nil && extractRelease(archive, t.TempDir(), "v1.2.0") == nil {
				t.Fatal("accepted wrong version")
			}
		})
	}
}

type fakeSource struct {
	release     *Release
	archive     []byte
	badChecksum bool
	entered     chan struct{}
	resume      chan struct{}
}

func (f *fakeSource) Latest(context.Context, bool) (*Release, error) { return f.release, nil }
func (f *fakeSource) Download(ctx context.Context, a Asset, dest string, _ int64) error {
	if a.Name == ArchiveName() {
		if f.entered != nil {
			close(f.entered)
			select {
			case <-f.resume:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return os.WriteFile(dest, f.archive, 0600)
	}
	sum := sha256.Sum256(f.archive)
	if f.badChecksum {
		sum = sha256.Sum256([]byte("wrong"))
	}
	return os.WriteFile(dest, []byte(fmt.Sprintf("%x  %s\n", sum, ArchiveName())), 0600)
}

func fixture(t *testing.T) (*Manager, *fakeSource) {
	t.Helper()
	r := Runtime{Root: t.TempDir()}
	write(t, filepath.Join(r.Root, "releases/old/manifest.json"), manifest("v1.0.0"))
	if err := os.MkdirAll(filepath.Join(r.Root, "ready"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := r.activate("releases/old"); err != nil {
		t.Fatal(err)
	}
	f := &fakeSource{archive: bundle(t, "v1.1.0", nil), release: &Release{Version: "v1.1.0", Compatible: true, Assets: []Asset{{ID: 1, Name: ArchiveName(), Size: 10}, {ID: 2, Name: "checksums.txt", Size: 10}}}}
	m := &Manager{Runtime: r, Source: f, Version: "v1.0.0", Repository: DefaultRepository, Managed: true, Context: t.Context()}
	return m, f
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestUpdateStagesAllComponentsAndChecksBeforeActivation(t *testing.T) {
	m, f := fixture(t)
	f.entered, f.resume = make(chan struct{}), make(chan struct{})
	if _, err := m.Start(t.Context(), "v1.0.9"); !errors.Is(err, ErrStale) {
		t.Fatalf("stale: %v", err)
	}
	if _, err := m.Start(t.Context(), "v1.1.0"); err != nil {
		t.Fatal(err)
	}
	<-f.entered
	if _, err := m.Start(t.Context(), "v1.1.0"); !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent update: %v", err)
	}
	if current, _ := m.Runtime.current(); current != "releases/old" {
		t.Fatal("activated before validation")
	}
	close(f.resume)
	eventually(t, func() bool { s, _ := m.Runtime.readState(); return s.Job != nil && s.Job.Phase == "restarting" })
	s, _ := m.Runtime.readState()
	if err := validateRelease(filepath.Join(m.Runtime.Root, s.Target), "v1.1.0"); err != nil {
		t.Fatal(err)
	}
	if err := m.Runtime.reconcile(time.Now()); err != nil {
		t.Fatal(err)
	}
	if current, _ := m.Runtime.current(); current != s.Target {
		t.Fatal("did not activate validated release")
	}
	for _, name := range []string{"api", "worker", "edge"} {
		if err := m.Runtime.report(name, s.Target, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Runtime.reconcile(time.Now()); err != nil {
		t.Fatal(err)
	}
	if s, _ := m.Runtime.readState(); s.Job.Phase != "succeeded" {
		t.Fatalf("job: %+v", s.Job)
	}
}

func TestBadChecksumKeepsOriginalVersion(t *testing.T) {
	m, f := fixture(t)
	f.badChecksum = true
	if _, err := m.Start(t.Context(), "v1.1.0"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { s, _ := m.Runtime.readState(); return s.Job.Phase == "failed" })
	if current, _ := m.Runtime.current(); current != "releases/old" {
		t.Fatal("bad package replaced running version")
	}
}

func TestHealthCheckRollbackAndInterruptedDownload(t *testing.T) {
	m, _ := fixture(t)
	if _, err := m.Start(t.Context(), "v1.1.0"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { s, _ := m.Runtime.readState(); return s.Job.Phase == "restarting" })
	s, _ := m.Runtime.readState()
	// 只有两个服务恢复健康也不能标记升级成功。
	for _, name := range []string{"api", "worker"} {
		if err := m.Runtime.report(name, s.Target, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Runtime.reconcile(s.Deadline.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if current, _ := m.Runtime.current(); current != s.Previous {
		t.Fatal("did not roll back")
	}
	for _, name := range []string{"api", "worker", "edge"} {
		if err := m.Runtime.report(name, s.Previous, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Runtime.reconcile(time.Now()); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Runtime.readState()
	if s.Job.Phase != "rolled_back" {
		t.Fatalf("phase: %s", s.Job.Phase)
	}
	s.Job.Phase = "rollback_failed"
	if err := m.Runtime.save(s); err != nil {
		t.Fatal(err)
	}
	if m.Status().Enabled {
		t.Fatal("update enabled before rollback recovery")
	}
	if err := m.Runtime.reconcile(time.Now()); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Runtime.readState()
	if s.Job.Phase != "rolled_back" {
		t.Fatal("healthy original release did not recover rollback status")
	}
	s.Job.Phase = "downloading"
	if err := m.Runtime.save(s); err != nil {
		t.Fatal(err)
	}
	if err := m.RecoverInterrupted(); err != nil {
		t.Fatal(err)
	}
	if s, _ := m.Runtime.readState(); s.Job.Phase != "failed" {
		t.Fatal("interrupted task remains active")
	}
}

func TestVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		target, current string
		want            bool
	}{
		{"v1.10.0", "v1.9.1", true}, {"v1.0.0", "1.0.0-rc.1", true},
		{"v1.0.0", "v1.0.0", false}, {"v1.0.0", "v1.1.0", false},
		{"v2.0.0-rc.1", "v1.0.0", false}, {"v2.0.0", "dev", false},
		{"v01.1.0", "v1.0.0", false}, {"../../pwn", "v1.0.0", false},
	} {
		if got := Newer(tc.target, tc.current); got != tc.want {
			t.Errorf("Newer(%q,%q) = %v", tc.target, tc.current, got)
		}
	}
}

func TestSeedPreservesHotUpdateAndAcceptsNewerImage(t *testing.T) {
	r := Runtime{Root: t.TempDir()}
	makeRelease := func(version string) string {
		t.Helper()
		archive := filepath.Join(t.TempDir(), "archive.tar.gz")
		write(t, archive, bundle(t, version, nil))
		dir := t.TempDir()
		if err := extractRelease(archive, dir, version); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	image := makeRelease("v1.0.0")
	if err := r.Seed(image); err != nil {
		t.Fatal(err)
	}
	first, err := r.current()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRelease(filepath.Join(r.Root, first), "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	hot := makeRelease("v1.1.0")
	if err := copyTree(hot, filepath.Join(r.Root, "releases/hot")); err != nil {
		t.Fatal(err)
	}
	if err := r.activate("releases/hot"); err != nil {
		t.Fatal(err)
	}
	if err := r.Seed(image); err != nil {
		t.Fatal(err)
	}
	if current, _ := r.current(); current != "releases/hot" {
		t.Fatal("old image overwrote persisted hot update")
	}
	if err := r.Seed(makeRelease("v1.2.0")); err != nil {
		t.Fatal(err)
	}
	current, err := r.current()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRelease(filepath.Join(r.Root, current), "v1.2.0"); err != nil {
		t.Fatal("newer image did not take precedence:", err)
	}
}

func TestRunnerHelperProcess(t *testing.T) {
	if os.Getenv("TM_TEST_RUNNER_HELPER") != "1" {
		return
	}
	addr := os.Getenv("TM_TEST_HEALTH_ADDR")
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	srv := &http.Server{Addr: addr, ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		data, err := os.ReadFile(filepath.Join(os.Getenv("TM_HTTP_WEB_DIR"), "index.html"))
		if err != nil || strings.Contains(string(data), "unhealthy") {
			w.WriteHeader(503)
		}
		_, _ = w.Write(data)
	})}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
}

// 真实子进程验证信号转发、三个服务同步切换，以及 api 无法启动时启动器仍可回退。
func TestRunnersSwitchAndRollback(t *testing.T) {
	t.Setenv("TM_TEST_RUNNER_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := Runtime{Root: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(r.Root, "ready"), 0755); err != nil {
		t.Fatal(err)
	}
	addrs := map[string]string{}
	for _, name := range []string{"api", "worker", "edge"} {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addrs[name] = listener.Addr().String()
		_ = listener.Close()
	}
	for _, version := range []string{"old", "new", "broken"} {
		for _, name := range []string{"api", "worker", "edge"} {
			script := fmt.Sprintf("#!/bin/sh\nexport TM_TEST_HEALTH_ADDR='%s'\nexec '%s' -test.run='^TestRunnerHelperProcess$'\n", addrs[name], executable)
			if version == "broken" && name == "api" {
				script = "#!/bin/sh\nexit 1\n"
			}
			write(t, filepath.Join(r.Root, "releases", version, "bin", name), []byte(script))
		}
		write(t, filepath.Join(r.Root, "releases", version, "web/index.html"), []byte(version))
	}
	if err := r.activate("releases/old"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 3)
	for name, addr := range addrs {
		go func() { done <- r.Run(ctx, name, addr, nil, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	}
	defer func() {
		cancel()
		for range 3 {
			if err := <-done; err != nil {
				t.Errorf("runner: %v", err)
			}
		}
	}()
	eventually(t, func() bool { return r.allHealthy("releases/old", time.Now()) })
	job := state{Job: &Job{ID: "new", Version: "v1.1.0", Phase: "restarting"}, Previous: "releases/old", Target: "releases/new", Deadline: time.Now().Add(15 * time.Second)}
	if err := r.save(job); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { s, _ := r.readState(); return s.Job != nil && s.Job.Phase == "succeeded" })
	if current, _ := r.current(); current != "releases/new" {
		t.Fatal("wrong running version")
	}
	job = state{Job: &Job{ID: "broken", Version: "v1.2.0", Phase: "restarting"}, Previous: "releases/new", Target: "releases/broken", Deadline: time.Now().Add(5 * time.Second)}
	if err := r.save(job); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { s, _ := r.readState(); return s.Job.Phase == "rolled_back" })
	if current, _ := r.current(); current != "releases/new" {
		t.Fatal("rollback did not restore last good version")
	}
}
