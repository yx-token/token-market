// Command dev-proxy exposes one local development origin for the embedded
// market and the sub2api admin UI. It is intentionally not a production proxy.
package main

import (
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "listen address")
	target := flag.String("target", "http://127.0.0.1:18080", "sub2api engine URL")
	adminDir := flag.String("admin-dir", "", "built sub2api admin frontend directory")
	adminKeyFile := flag.String("admin-key-file", "", "file containing local sub2api Admin API key")
	flag.Parse()
	u, err := url.Parse(*target)
	if err != nil || u.Scheme == "" || u.Host == "" {
		log.Fatalf("invalid target URL: %q", *target)
	}
	adminKey := ""
	if *adminKeyFile != "" {
		key, err := os.ReadFile(*adminKeyFile)
		if err != nil {
			log.Fatalf("read admin API key: %v", err)
		}
		adminKey = strings.TrimSpace(string(key))
	}
	h := newHandler(u, adminKey, *adminDir)
	srv := &http.Server{
		Addr:              *listen,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	log.Printf("dev-proxy listening on %s -> %s", *listen, *target)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func newHandler(target *url.URL, adminKey, adminDir string) http.Handler {
	proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		path := pr.In.URL.Path
		if isMarketAdminPath(path) {
			rest := strings.TrimPrefix(path, "/api/admin/")
			if rest == "sub2api/status" {
				rest = "status"
			}
			pr.Out.URL.Path, pr.Out.URL.RawPath = "/api/v1/admin/market/"+rest, ""
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("x-api-key")
			if adminKey != "" {
				pr.Out.Header.Set("x-api-key", adminKey)
			}
		}
		pr.SetURL(target)
		// 与原 Director 行为一致：上游看到的仍是浏览器访问的 Host，另附 X-Forwarded-Host / Proto / For
		pr.Out.Host = pr.In.Host
		pr.SetXForwarded()
	}}
	guardedProxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMarketAdminPath(r.URL.Path) && !sameOriginAdminRequest(r) {
			http.Error(w, "cross-origin admin request rejected", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	})
	dir := strings.TrimSpace(adminDir)
	if dir == "" {
		return guardedProxy
	}
	files := http.FileServer(http.Dir(filepath.Join(dir, "admin-assets")))
	admin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			switch r.URL.Path {
			case "/engine", "/engine/", "/console", "/console/":
				http.Redirect(w, r, "/admin/dashboard", http.StatusFound)
				return
			case "/admin/open-engine":
				http.Redirect(w, r, "/open-admin", http.StatusFound)
				return
			}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			guardedProxy.ServeHTTP(w, r)
			return
		}
		clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/admin-assets/"))
		if strings.HasPrefix(r.URL.Path, "/admin-assets/") && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/" + filepath.ToSlash(clean)
			files.ServeHTTP(w, r2)
			return
		}
		if r.URL.Path == "/xianmao-logo.svg" || r.URL.Path == "/logo.svg" {
			file := filepath.Join(dir, filepath.Base(r.URL.Path))
			if _, err := os.Stat(file); err == nil {
				http.ServeFile(w, r, file)
				return
			}
		}
		if r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/") {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		guardedProxy.ServeHTTP(w, r)
	})
	return admin
}

func isMarketAdminPath(path string) bool {
	return strings.HasPrefix(path, "/api/admin/") && !strings.HasPrefix(path, "/api/admin/engine/")
}

func sameOriginAdminRequest(r *http.Request) bool {
	if fetchSite := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")); fetchSite != "" &&
		!strings.EqualFold(fetchSite, "same-origin") && !strings.EqualFold(fetchSite, "none") {
		return false
	}
	if rawOrigin := strings.TrimSpace(r.Header.Get("Origin")); rawOrigin != "" {
		u, err := url.Parse(rawOrigin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
			u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" ||
			!strings.EqualFold(u.Host, r.Host) {
			return false
		}
	}
	return true
}
