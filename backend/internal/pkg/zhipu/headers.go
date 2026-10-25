package zhipu

import (
	"net/http"
	"runtime"
	"time"
)

// SourceOptions describes the server acting as a ZCode CLI client.
type SourceOptions struct {
	Version     string
	Language    string
	Timezone    string
	Platform    string
	OSCategory  string
	OSVersion   string
	DeviceMID   string
	ZCodeOrigin string
}

func SourceHeaders(opts SourceOptions) http.Header {
	if opts.Version == "" {
		opts.Version = ClientVersion
	}
	if opts.Language == "" {
		opts.Language = "zh-CN"
	}
	if opts.Timezone == "" {
		opts.Timezone = defaultTimezone(time.Local)
	}
	if opts.Platform == "" {
		platform, arch := runtime.GOOS, runtime.GOARCH
		if platform == "windows" {
			platform = "win32"
		}
		if arch == "amd64" {
			arch = "x64"
		}
		opts.Platform = platform + "-" + arch
	}
	if opts.OSCategory == "" {
		opts.OSCategory = runtime.GOOS
		if opts.OSCategory == "darwin" {
			opts.OSCategory = "macos"
		}
	}
	if opts.OSVersion == "" {
		opts.OSVersion = "unknown"
	}
	if opts.ZCodeOrigin == "" {
		opts.ZCodeOrigin = ZCodeOrigin
	}
	h := make(http.Header)
	for k, v := range map[string]string{
		"User-Agent": "ZCode/" + opts.Version, "X-ZCode-App-Version": opts.Version,
		"X-ZCode-Agent": "glm", "X-Title": "Z Code@cli", "X-Release-Channel": "production",
		"X-Client-Language": opts.Language, "X-Client-Timezone": opts.Timezone,
		"X-Platform": opts.Platform, "X-Os-Category": opts.OSCategory, "X-Os-Version": opts.OSVersion,
		"HTTP-Referer": opts.ZCodeOrigin,
	} {
		h.Set(k, v)
	}
	if opts.DeviceMID != "" {
		h.Set("X-Device-Mid", opts.DeviceMID)
	}
	return h
}

func defaultTimezone(location *time.Location) string {
	if location != nil && location.String() != "Local" {
		name := location.String()
		if _, err := time.LoadLocation(name); err == nil {
			return name
		}
	}
	return "Asia/Shanghai"
}
