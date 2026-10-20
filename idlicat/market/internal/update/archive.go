package update

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Manifest 固定更新包格式；启动器独立于应用版本，协议变更需要镜像升级。
type Manifest struct {
	Schema  int    `json:"schema"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

func verifyChecksum(archive, checksum string) error {
	data, err := os.ReadFile(checksum)
	if err != nil {
		return err
	}
	scan := bufio.NewScanner(strings.NewReader(string(data)))
	want := ""
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == filepath.Base(archive) {
			if want != "" {
				return errors.New("校验文件包含重复条目")
			}
			want = fields[0]
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}
	if digest, err := hex.DecodeString(want); err != nil || len(digest) != sha256.Size {
		return errors.New("更新包缺少有效的 SHA-256 校验值")
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want) {
		return errors.New("更新包 SHA-256 校验失败")
	}
	return nil
}

// extractRelease 不接受链接、特殊文件、路径穿越或包外文件，并限制解压总量。
func extractRelease(archive, dest, version string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var total int64
	seen := map[string]bool{}
	for count := 0; ; count++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(strings.TrimPrefix(h.Name, "./"), "/")
		if name == "." && h.Typeflag == tar.TypeDir {
			continue
		}
		if count > 10000 || path.Clean(name) != name || strings.Contains(name, "\\") || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return errors.New("更新包路径或文件数量无效")
		}
		allowed := name == "manifest.json" || name == "bin" || name == "web" || strings.HasPrefix(name, "web/")
		for _, bin := range []string{"api", "worker", "edge", "s2a-compat"} {
			allowed = allowed || name == "bin/"+bin
		}
		if !allowed || (h.Typeflag != tar.TypeDir && h.Typeflag != tar.TypeReg) {
			return errors.New("更新包包含不允许的文件或链接")
		}
		if seen[name] {
			return errors.New("更新包包含重复路径")
		}
		seen[name] = true
		if h.Size < 0 || h.Size > maxArchiveSize || total+h.Size > 1<<30 {
			return errors.New("更新包解压大小超过限制")
		}
		total += h.Size
		target := filepath.Join(dest, filepath.FromSlash(name))
		if h.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0755
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return err
		}
	}
	return validateRelease(dest, version)
}

func validateRelease(dir, version string) error {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return errors.New("更新包缺少发布清单")
	}
	var m Manifest
	if json.Unmarshal(data, &m) != nil || m.Schema != 1 || m.Version != version || m.OS != runtime.GOOS || m.Arch != runtime.GOARCH {
		return errors.New("更新包版本、平台或协议不匹配")
	}
	for _, name := range []string{"bin/api", "bin/worker", "bin/edge", "bin/s2a-compat", "web/index.html"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("更新包缺少必要文件 %s", name)
		}
	}
	return nil
}
