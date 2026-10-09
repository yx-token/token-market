package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type Status struct {
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	Platform   string `json:"platform"`
	Repository string `json:"repository"`
	Enabled    bool   `json:"enabled"`
	Reason     string `json:"reason,omitempty"`
	Job        *Job   `json:"job,omitempty"`
}

type Check struct {
	Release         *Release `json:"release"`
	UpdateAvailable bool     `json:"update_available"`
}

type Controller interface {
	Status() Status
	Check(context.Context, bool) (Check, error)
	Start(context.Context, string) (*Job, error)
}

type Manager struct {
	Runtime    Runtime
	Source     Source
	Version    string
	Commit     string
	Repository string
	Managed    bool
	Context    context.Context
	Log        *slog.Logger
}

func (m *Manager) Status() Status {
	s := Status{Version: m.Version, Commit: m.Commit, Platform: runtime.GOOS + "/" + runtime.GOARCH, Repository: m.Repository, Enabled: true}
	switch {
	case !m.Managed || m.Runtime.Root == "":
		s.Enabled, s.Reason = false, ErrDisabled.Error()
	case !versionPattern.MatchString(m.Version):
		s.Enabled, s.Reason = false, "开发构建不支持版本升级，请先部署正式发布镜像"
	default:
		if _, err := m.Runtime.current(); err != nil {
			s.Enabled, s.Reason = false, "更新运行目录不可用"
		}
		if !m.Runtime.writable() {
			s.Enabled, s.Reason = false, "更新运行目录不可写，请检查共享卷权限"
		}
		state, err := m.Runtime.readState()
		if err != nil {
			s.Enabled, s.Reason = false, "无法读取更新任务状态"
		} else {
			s.Job = state.Job
		}
		if s.Job.Active() {
			s.Enabled, s.Reason = false, ErrBusy.Error()
		}
		if s.Job != nil && s.Job.Phase == "rollback_failed" {
			s.Enabled, s.Reason = false, "原版本服务健康检查未通过，请先恢复服务"
		}
	}
	return s
}

func (m *Manager) Check(ctx context.Context, force bool) (Check, error) {
	r, err := m.Source.Latest(ctx, force)
	if err != nil {
		return Check{}, err
	}
	if r == nil {
		return Check{}, errors.New("最新发布信息为空")
	}
	return Check{r, Newer(r.Version, m.Version)}, nil
}

func (m *Manager) Start(ctx context.Context, version string) (*Job, error) {
	status := m.Status()
	if !status.Enabled {
		if status.Job.Active() {
			return nil, ErrBusy
		}
		return nil, ErrDisabled
	}
	f, err := m.Runtime.lock()
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			unlock(f)
		}
	}()
	s, err := m.Runtime.readState()
	if err != nil {
		return nil, err
	}
	if s.Job.Active() {
		return nil, ErrBusy
	}
	r, err := m.Source.Latest(ctx, true)
	if err != nil {
		return nil, err
	}
	if r == nil || r.Version != version {
		return nil, ErrStale
	}
	if !Newer(r.Version, m.Version) {
		return nil, ErrNoUpdate
	}
	if !r.Compatible {
		return nil, errors.New("此版本缺少当前平台的更新包或 SHA-256 校验文件")
	}
	previous, err := m.Runtime.current()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	id := fmt.Sprintf("%d", now.UnixNano())
	s = state{Job: &Job{ID: id, Version: version, Phase: "queued", Message: "更新任务已创建", StartedAt: now, UpdatedAt: now}, Previous: previous, Target: "releases/" + version + "-" + id}
	if err := m.Runtime.save(s); err != nil {
		return nil, err
	}
	job := *s.Job
	transferred = true
	go func() {
		defer unlock(f)
		base := m.Context
		if base == nil {
			base = context.Background()
		}
		ctx, cancel := context.WithTimeout(base, 10*time.Minute)
		defer cancel()
		if err := m.perform(ctx, r, &s); err != nil {
			if m.Log != nil {
				m.Log.Error("system update failed", "version", version, "err", err)
			}
			s.Job.Phase, s.Job.Message, s.Job.UpdatedAt = "failed", "更新失败，原版本继续运行；请检查服务日志后重试", time.Now().UTC()
			if saveErr := m.Runtime.save(s); saveErr != nil && m.Log != nil {
				m.Log.Error("save update failure", "err", saveErr)
			}
		}
	}()
	return &job, nil
}

func (m *Manager) phase(s *state, phase, message string) error {
	s.Job.Phase, s.Job.Message, s.Job.UpdatedAt = phase, message, time.Now().UTC()
	return m.Runtime.save(*s)
}

func (m *Manager) perform(ctx context.Context, r *Release, s *state) error {
	tmp, err := os.MkdirTemp(m.Runtime.Root, ".update-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	archive := filepath.Join(tmp, ArchiveName())
	checksum := filepath.Join(tmp, "checksums.txt")
	if err := m.phase(s, "downloading", "正在下载更新包"); err != nil {
		return err
	}
	a, c := findAsset(r, ArchiveName()), findAsset(r, "checksums.txt")
	if a == nil || c == nil {
		return errors.New("更新附件不完整")
	}
	if err := m.Source.Download(ctx, *a, archive, maxArchiveSize); err != nil {
		return err
	}
	if err := m.Source.Download(ctx, *c, checksum, 1<<20); err != nil {
		return err
	}
	if err := m.phase(s, "checking", "正在校验并解压更新包"); err != nil {
		return err
	}
	if err := verifyChecksum(archive, checksum); err != nil {
		return err
	}
	dir := filepath.Join(tmp, "release")
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}
	if err := extractRelease(archive, dir, r.Version); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(dir, filepath.Join(m.Runtime.Root, s.Target)); err != nil {
		return err
	}
	// 保存可恢复的事务后由独立启动器切换；下载进程不会杀死自身。
	s.Deadline = time.Now().UTC().Add(90 * time.Second)
	return m.phase(s, "restarting", "正在切换版本并重启服务，等待健康检查")
}

// RecoverInterrupted 只在 api 启动时调用，清理进程中断的下载任务；切换与回退由启动器接手。
func (m *Manager) RecoverInterrupted() error {
	if !m.Managed {
		return nil
	}
	f, err := m.Runtime.lock()
	if errors.Is(err, ErrBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock(f)
	s, err := m.Runtime.readState()
	if err != nil || !s.Job.Active() || s.Job.Phase == "restarting" || s.Job.Phase == "rolling_back" {
		return err
	}
	return m.phase(&s, "failed", "更新下载被服务重启中断，原版本继续运行，请重试")
}
