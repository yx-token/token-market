// Package displaycode 生成界面展示编号：日期（上海时区 YYYYMMDD）+ 随机小写字母数字。
package displaycode

import (
	"crypto/rand"
	"fmt"
	"time"
)

const digits = "0123456789"

// New 生成形如 2026102719382746 的编号（日期 + 纯数字随机串，无 # 前缀）。
func New() string {
	return NewAt(time.Now())
}

// NewAt 按给定时间生成编号（用于测试或按业务时间落号）。
func NewAt(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	day := t.In(loc).Format("20060102")
	return day + randomDigits(8)
}

func randomDigits(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// 极罕见：退化为时间纳秒派生，仍保证长度
		ns := time.Now().UnixNano()
		for i := 0; i < n; i++ {
			buf[i] = digits[(ns+int64(i)*31)%10]
		}
		return string(buf)
	}
	for i := range buf {
		buf[i] = digits[int(buf[i])%10]
	}
	return string(buf)
}

// MustNew 与 New 相同；保留别名便于调用方语义。
func MustNew() string {
	c := New()
	if c == "" {
		panic(fmt.Errorf("displaycode: empty"))
	}
	return c
}
