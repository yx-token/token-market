//go:build !unix

package update

import "os"

// 站内更新依赖 flock 与符号链接，仅在 market-runner 的 Linux 容器中启用。
func (r Runtime) writable() bool { return false }

func (r Runtime) lock() (*os.File, error) { return nil, ErrDisabled }

func unlock(f *os.File) { _ = f.Close() }
