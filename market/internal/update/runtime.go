package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrBusy = errors.New("已有更新任务正在执行")
var ErrDisabled = errors.New("当前运行环境未启用站内更新，请使用 market-runner 启动所有服务")
var ErrNoUpdate = errors.New("当前没有可用的新版本")
var ErrStale = errors.New("最新版本已变化，请重新检查更新")

type Job struct {
	ID        string    `json:"id"`
	Version   string    `json:"version"`
	Phase     string    `json:"phase"`
	Message   string    `json:"message"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (j *Job) Active() bool {
	return j != nil && j.Phase != "succeeded" && j.Phase != "failed" && j.Phase != "rolled_back" && j.Phase != "rollback_failed"
}

type state struct {
	Job      *Job      `json:"job"`
	Target   string    `json:"target"`
	Previous string    `json:"previous"`
	Deadline time.Time `json:"deadline"`
}

type heartbeat struct {
	Release string    `json:"release"`
	Healthy bool      `json:"healthy"`
	At      time.Time `json:"at"`
}

// Runtime 在共享卷内保存版本指针、任务及各启动器的健康状态。
type Runtime struct{ Root string }

func atomicJSON(file string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), ".write-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(f.Name(), file)
}

func (r Runtime) readState() (state, error) {
	var s state
	data, err := os.ReadFile(filepath.Join(r.Root, "job.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	return s, err
}

func (r Runtime) save(s state) error { return atomicJSON(filepath.Join(r.Root, "job.json"), s) }

func (r Runtime) current() (string, error) {
	link, err := os.Readlink(filepath.Join(r.Root, "current"))
	if err != nil {
		return "", err
	}
	if !validReleasePath(link) {
		return "", errors.New("运行版本指针无效")
	}
	return link, nil
}

func validReleasePath(link string) bool {
	return filepath.Dir(link) == "releases" && filepath.Base(link) != "." && filepath.Base(link) != ".." && !strings.Contains(link, "\\")
}

func (r Runtime) activate(link string) error {
	if !validReleasePath(link) {
		return errors.New("运行版本指针无效")
	}
	if info, err := os.Stat(filepath.Join(r.Root, link)); err != nil || !info.IsDir() {
		return errors.New("目标版本目录不存在")
	}
	tmp, err := os.MkdirTemp(r.Root, ".link-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := os.Symlink(link, filepath.Join(tmp, "current")); err != nil {
		return err
	}
	return os.Rename(filepath.Join(tmp, "current"), filepath.Join(r.Root, "current"))
}

func (r Runtime) report(component, release string, healthy bool) error {
	return atomicJSON(filepath.Join(r.Root, "ready", component+".json"), heartbeat{release, healthy, time.Now().UTC()})
}

func (r Runtime) allHealthy(release string, now time.Time) bool {
	for _, name := range []string{"api", "worker", "edge"} {
		data, err := os.ReadFile(filepath.Join(r.Root, "ready", name+".json"))
		var h heartbeat
		if err != nil || json.Unmarshal(data, &h) != nil || !h.Healthy || h.Release != release || now.Sub(h.At) > 10*time.Second {
			return false
		}
	}
	return true
}

// reconcile 由 api 启动器执行；持久化任务使应用重启不会丢失更新及回退进度。
func (r Runtime) reconcile(now time.Time) error {
	f, err := r.lock()
	if errors.Is(err, ErrBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock(f)
	s, err := r.readState()
	if err != nil || s.Job == nil {
		return err
	}
	if s.Job.Phase == "rollback_failed" {
		if current, err := r.current(); err == nil && current == s.Previous && r.allHealthy(s.Previous, now) {
			s.Job.Phase, s.Job.Message, s.Job.UpdatedAt = "rolled_back", "原版本服务已恢复，回退完成", now
			return r.save(s)
		}
		return nil
	}
	if !s.Job.Active() {
		return nil
	}
	if s.Job.Phase != "restarting" && s.Job.Phase != "rolling_back" {
		return nil
	}
	link := s.Target
	if s.Job.Phase == "rolling_back" {
		link = s.Previous
	}
	// 如果应用在保存任务后、切换指针前退出，启动器接手完成切换。
	current, err := r.current()
	if err != nil {
		return err
	}
	if current != link {
		if err := r.activate(link); err != nil {
			return err
		}
	}
	if r.allHealthy(link, now) {
		if s.Job.Phase == "rolling_back" {
			s.Job.Phase, s.Job.Message = "rolled_back", "新版本健康检查失败，已恢复原版本"
		} else {
			s.Job.Phase, s.Job.Message = "succeeded", "三个服务已切换至新版本，健康检查通过"
		}
	} else if now.After(s.Deadline) {
		if s.Job.Phase == "rolling_back" {
			s.Job.Phase, s.Job.Message = "rollback_failed", "已切回旧版本，但服务健康检查未通过，请检查服务日志"
		} else {
			s.Job.Phase, s.Job.Message = "rolling_back", "新版本健康检查失败，正在恢复原版本"
			s.Deadline = now.Add(90 * time.Second)
			if err := r.activate(s.Previous); err != nil {
				return err
			}
		}
	} else {
		return nil
	}
	s.Job.UpdatedAt = now
	return r.save(s)
}
