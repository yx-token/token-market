package compat

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// MigrationsDir 是 sub2api 仓库中迁移脚本所在目录。
const MigrationsDir = "backend/migrations"

// MigrationSet 是某个 sub2api 版本的全部迁移脚本。
type MigrationSet struct {
	Desc     string
	Names    []string          // 按文件名排序，只含 .sql
	Contents map[string][]byte // 文件名 → 内容
}

// Checksum 与 sub2api 迁移执行器一致：文件内容的 SHA-256 十六进制。
func Checksum(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// LoadGitMigrations 从 sub2api 本地仓库读取 ref（标签、分支或提交）对应的迁移脚本。
func LoadGitMigrations(ctx context.Context, repo, ref string) (*MigrationSet, error) {
	ls := exec.CommandContext(ctx, "git", "-C", repo, "ls-tree", "--name-only", ref, MigrationsDir+"/")
	out, err := ls.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-tree %s: %w", ref, gitErr(err))
	}
	var paths []string
	for _, p := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasSuffix(p, ".sql") && path.Dir(p) == MigrationsDir {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no migrations found at %s:%s", ref, MigrationsDir)
	}
	contents, err := catFiles(ctx, repo, ref, paths)
	if err != nil {
		return nil, err
	}
	set := &MigrationSet{Desc: "git " + ref, Contents: make(map[string][]byte, len(paths))}
	for _, p := range paths {
		name := path.Base(p)
		set.Names = append(set.Names, name)
		set.Contents[name] = contents[p]
	}
	sort.Strings(set.Names)
	return set, nil
}

// catFiles 用一个 git cat-file --batch 进程批量读取文件，避免几百次进程启动。
func catFiles(ctx context.Context, repo, ref string, paths []string) (map[string][]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "cat-file", "--batch")
	var input bytes.Buffer
	for _, p := range paths {
		fmt.Fprintf(&input, "%s:%s\n", ref, p)
	}
	cmd.Stdin = &input
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	r := bufio.NewReader(stdout)
	out := make(map[string][]byte, len(paths))
	for _, p := range paths {
		header, err := r.ReadString('\n')
		if err != nil {
			_ = cmd.Wait()
			return nil, fmt.Errorf("git cat-file %s: %w %s", p, err, stderr.String())
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[1] != "blob" {
			_ = cmd.Wait()
			return nil, fmt.Errorf("git cat-file %s: unexpected header %q", p, strings.TrimSpace(header))
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			_ = cmd.Wait()
			return nil, fmt.Errorf("git cat-file %s: bad size: %w", p, err)
		}
		buf := make([]byte, size+1) // 内容后跟一个换行
		if _, err := io.ReadFull(r, buf); err != nil {
			_ = cmd.Wait()
			return nil, fmt.Errorf("git cat-file %s: %w", p, err)
		}
		out[p] = buf[:size]
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w %s", err, stderr.String())
	}
	return out, nil
}

// LoadDirMigrations 从目录读取迁移脚本（例如解压的发布包）。
func LoadDirMigrations(dir string) (*MigrationSet, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	set := &MigrationSet{Desc: "dir " + dir, Contents: map[string][]byte{}}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		set.Names = append(set.Names, e.Name())
		set.Contents[e.Name()] = raw
	}
	sort.Strings(set.Names)
	if len(set.Names) == 0 {
		return nil, fmt.Errorf("no migrations found in %s", dir)
	}
	return set, nil
}

// Applied 是当前 sub2api 已执行的迁移：文件名 → checksum。
type Applied struct {
	Desc      string
	Checksums map[string]string
}

// AppliedFromSet 把某个版本的全部迁移视为已执行（用"当前版本的 git ref"代替读生产库时使用）。
func AppliedFromSet(set *MigrationSet) Applied {
	a := Applied{Desc: set.Desc, Checksums: make(map[string]string, len(set.Names))}
	for _, name := range set.Names {
		a.Checksums[name] = Checksum(set.Contents[name])
	}
	return a
}

func (a Applied) Names() []string {
	names := make([]string, 0, len(a.Checksums))
	for name := range a.Checksums {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func gitErr(err error) error {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}
