package userclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginRefreshAndCreateKey(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = nil
		_ = json.Unmarshal(raw, &gotBody)
		switch r.URL.Path {
		case "/api/v1/auth/login":
			_, _ = io.WriteString(w, `{"code":0,"data":{"access_token":"a1","refresh_token":"r1","expires_in":3600,"token_type":"Bearer"}}`)
		case "/api/v1/auth/refresh":
			_, _ = io.WriteString(w, `{"code":0,"data":{"access_token":"a2","refresh_token":"r2","expires_in":3600}}`)
		case "/api/v1/keys":
			gotAuth = r.Header.Get("Authorization")
			_, _ = io.WriteString(w, `{"code":0,"data":{"id":9,"user_id":3,"key":"tm_internal_0123456789","group_id":7,"status":"active"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, 5*time.Second, nil)
	ctx := context.Background()

	tok, err := c.Login(ctx, "b1@shadow.local", "pw")
	if err != nil || tok.RefreshToken != "r1" {
		t.Fatalf("login: %v %+v", err, tok)
	}
	tok, err = c.Refresh(ctx, tok.RefreshToken)
	if err != nil || tok.RefreshToken != "r2" {
		t.Fatalf("refresh: %v %+v", err, tok)
	}
	key, err := c.CreateKey(ctx, tok.AccessToken, CreateKeyInput{Name: "tm-ik-1", GroupID: 7, CustomKey: "tm_internal_0123456789"})
	if err != nil {
		t.Fatal(err)
	}
	if key.ID != 9 || gotAuth != "Bearer a2" {
		t.Fatalf("key %+v auth %q", key, gotAuth)
	}
	if gotBody["custom_key"] != "tm_internal_0123456789" || gotBody["group_id"] != float64(7) {
		t.Fatalf("unexpected body %v", gotBody)
	}
}

func TestLoginWithoutRefreshTokenFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":0,"data":{"access_token":"a1"}}`)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, time.Second, nil).Login(context.Background(), "e", "p"); err == nil {
		t.Fatal("expected error when refresh token missing")
	}
}

func TestValidateCustomKey(t *testing.T) {
	if err := ValidateCustomKey("short"); err == nil {
		t.Error("short key accepted")
	}
	if err := ValidateCustomKey("tm_internal_key!!!!"); err == nil {
		t.Error("invalid characters accepted")
	}
	if err := ValidateCustomKey("tm_internal-KEY_0123"); err != nil {
		t.Errorf("valid key rejected: %v", err)
	}
}
