package update

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Seed 在首次启动时复制镜像中的应用；重建容器保留卷内已更新的版本。
// 显式部署更高版本镜像时，镜像版本优先；旧镜像不会覆盖已完成的站内升级。
func (r Runtime) Seed(imageDir string) error {
	if err := os.MkdirAll(filepath.Join(r.Root, "releases"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(r.Root, "ready"), 0755); err != nil {
		return err
	}
	f, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock(f)
	data, err := os.ReadFile(filepath.Join(imageDir, "manifest.json"))
	if err != nil {
		return err
	}
	var image Manifest
	if json.Unmarshal(data, &image) != nil {
		return errors.New("镜像发布清单无效")
	}
	if err := validateRelease(imageDir, image.Version); err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	link := fmt.Sprintf("releases/image-%x", hash[:12])
	current, err := r.current()
	if err == nil {
		oldData, readErr := os.ReadFile(filepath.Join(r.Root, current, "manifest.json"))
		var old Manifest
		if readErr != nil || json.Unmarshal(oldData, &old) != nil {
			return errors.New("当前版本发布清单不可用")
		}
		if !Newer(image.Version, old.Version) {
			return nil
		}
		state, readErr := r.readState()
		if readErr != nil {
			return readErr
		}
		if state.Job.Active() {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(filepath.Join(r.Root, link)); errors.Is(err, os.ErrNotExist) {
		tmp, err := os.MkdirTemp(r.Root, ".seed-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		for _, name := range []string{"bin/api", "bin/worker", "bin/edge", "bin/s2a-compat", "web", "manifest.json"} {
			if err := copyTree(filepath.Join(imageDir, name), filepath.Join(tmp, name)); err != nil {
				return err
			}
		}
		if err := os.Rename(tmp, filepath.Join(r.Root, link)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return r.activate(link)
}

func copyTree(source, dest string) error {
	return filepath.WalkDir(source, func(file string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("镜像包含非普通文件")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		in, err := os.Open(file)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		return errors.Join(copyErr, out.Close())
	})
}

// Run 作为稳定的容器入口运行应用，监听共享版本指针并转发终止信号。
// api 启动器协调健康检查与回退，应用退出后任务仍可继续。
func (r Runtime) Run(ctx context.Context, component, healthAddr string, args []string, log *slog.Logger) error {
	if component != "api" && component != "worker" && component != "edge" {
		return errors.New("组件应为 api、worker 或 edge")
	}
	_, port, err := net.SplitHostPort(healthAddr)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: time.Second}
	healthURL := "http://127.0.0.1:" + port + "/healthz"
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var child *exec.Cmd
	var done chan error
	active := ""
	stop := func() {
		if child == nil {
			return
		}
		_ = child.Process.Signal(syscall.SIGTERM)
		timer := time.NewTimer(40 * time.Second)
		defer timer.Stop()
		select {
		case <-done: //nolint:errcheck // child Wait result after stop is best-effort
		case <-timer.C:
			_ = child.Process.Kill()
			<-done //nolint:errcheck // child Wait result after kill is best-effort
		}
		child, done = nil, nil
	}
	defer func() { stop(); _ = r.report(component, active, false) }()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if component == "api" {
			if err := r.reconcile(time.Now().UTC()); err != nil {
				log.Error("update coordinator", "err", err)
			}
		}
		current, err := r.current()
		if err != nil {
			return err
		}
		if current != active || child == nil {
			_ = r.report(component, active, false)
			stop()
			active = current
			child = exec.Command(filepath.Join(r.Root, current, "bin", component), args...)
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			overrides := map[string]string{"TM_UPDATE_MANAGED": "1", "TM_UPDATE_RUNTIME_DIR": r.Root, "TM_HTTP_WEB_DIR": filepath.Join(r.Root, current, "web")}
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				if _, replaced := overrides[key]; !replaced {
					child.Env = append(child.Env, entry)
				}
			}
			for key, value := range overrides {
				child.Env = append(child.Env, key+"="+value)
			}
			if err := child.Start(); err != nil {
				log.Error("start component", "component", component, "err", err)
				child = nil
			} else {
				done = make(chan error, 1)
				process, result := child, done
				go func() { result <- process.Wait() }()
				log.Info("component started", "component", component, "release", current)
			}
		}
		if child != nil {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			healthy := err == nil && resp.StatusCode == http.StatusOK
			if resp != nil {
				_ = resp.Body.Close()
			}
			if err := r.report(component, active, healthy); err != nil {
				log.Error("update heartbeat", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-done:
			log.Warn("component exited", "component", component, "err", err)
			child, done = nil, nil
			_ = r.report(component, active, false)
			// 防止错误二进制导致启动循环；仍继续协调回退。
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		case <-ticker.C:
		}
	}
}
