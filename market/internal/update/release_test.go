package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, ContentLength: int64(len(body))}
}

func TestPrivateRepositoryDownloadsWithoutLeakingToken(t *testing.T) {
	g := NewGitHub(DefaultRepository, "server-only-test-token")
	calls := 0
	g.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			if req.URL.Host != "api.github.com" || req.Header.Get("Authorization") != "Bearer server-only-test-token" || req.Header.Get("Accept") != "application/octet-stream" {
				t.Fatal("private asset must use authenticated GitHub API")
			}
			resp := response(302, "")
			resp.Header.Set("Location", "https://release-assets.githubusercontent.com/signed-asset")
			return resp, nil
		}
		if req.URL.Host != "release-assets.githubusercontent.com" || req.Header.Get("Authorization") != "" {
			t.Fatal("token leaked to redirected host")
		}
		return response(200, "binary"), nil
	})
	if err := g.Download(t.Context(), Asset{ID: 123, Name: "archive", Size: 6}, filepath.Join(t.TempDir(), "archive"), 10); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls: %d", calls)
	}
}

func TestLatestReleaseRequiresCompatiblePackageAndCaches(t *testing.T) {
	g := NewGitHub(DefaultRepository, "")
	calls := 0
	body := fmt.Sprintf(`{"tag_name":"v1.2.0","assets":[{"id":1,"name":%q,"size":30},{"id":2,"name":"checksums.txt","size":10}]}`, ArchiveName())
	g.Client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, body), nil })
	r, err := g.Latest(context.Background(), false)
	if err != nil || !r.Compatible {
		t.Fatalf("release: %+v, %v", r, err)
	}
	if _, err := g.Latest(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("release cache missed")
	}
	if _, err := g.Latest(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("force did not refresh")
	}
	body = `{"tag_name":"v1.3.0","prerelease":true}`
	if _, err := g.Latest(t.Context(), true); err == nil {
		t.Fatal("accepted prerelease")
	}
}

func TestRejectsUntrustedRedirectAndIncompleteDownload(t *testing.T) {
	for _, raw := range []string{"http://github.com/file", "https://evil.github.com/file", "https://api.github.com:123/file", "https://secret@github.com/file", "https://example.com/file"} {
		u, _ := url.Parse(raw)
		if trustedURL(u) {
			t.Fatalf("trusted %s", raw)
		}
	}
	g := NewGitHub(DefaultRepository, "")
	g.Client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, "short"), nil })
	if err := g.Download(t.Context(), Asset{ID: 1, Size: 10}, filepath.Join(t.TempDir(), "archive"), 10); err == nil {
		t.Fatal("accepted incomplete download")
	}
	g.Client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		r := response(302, "")
		r.Header.Set("Location", "https://example.com/steal")
		return r, nil
	})
	if err := g.Download(t.Context(), Asset{ID: 1, Size: 10}, filepath.Join(t.TempDir(), "archive"), 10); err == nil {
		t.Fatal("accepted untrusted redirect")
	}
}
