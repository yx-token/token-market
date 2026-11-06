package mail

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
)

type memDB struct {
	data map[string][]byte
}

func (m *memDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	key, _ := args[0].(string)
	raw, ok := m.data[key]
	if !ok {
		return memRow{err: pgx.ErrNoRows}
	}
	return memRow{raw: raw}
}

func (m *memDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	if m.data == nil {
		m.data = map[string][]byte{}
	}
	key, _ := args[0].(string)
	raw, _ := args[1].([]byte)
	m.data[key] = append([]byte(nil), raw...)
	return pgconn.CommandTag{}, nil
}

type memRow struct {
	raw []byte
	err error
}

func (r memRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if p, ok := dest[0].(*[]byte); ok {
		*p = append([]byte(nil), r.raw...)
		return nil
	}
	return nil
}

func testKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	kr, err := crypto.NewKeyring(config.CryptoConfig{
		ActiveKeyID: "k1",
		Keys:        map[string]string{"k1": base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

func TestBootFromEnv(t *testing.T) {
	on, mode := BootFromEnv("dev", "").inferred()
	if !on || mode != ModeDev {
		t.Fatalf("dev: %v %s", on, mode)
	}
	on, mode = BootFromEnv("re_abc", "").inferred()
	if !on || mode != ModeResend {
		t.Fatalf("resend: %v %s", on, mode)
	}
	on, mode = BootFromEnv("off", "").inferred()
	if on || mode != ModeOff {
		t.Fatalf("off: %v %s", on, mode)
	}
}

func TestUpdateAndResolveHotReload(t *testing.T) {
	db := &memDB{}
	kr := testKeyring(t)
	boot := Boot{APIKey: "dev", From: "闲猫 <dev@example.com>"}

	v, err := LoadView(context.Background(), db, boot)
	if err != nil || !v.Enabled || v.Mode != ModeDev || v.Source != "boot" {
		t.Fatalf("boot view: %+v err=%v", v, err)
	}
	enabled, sender, err := ResolveSender(context.Background(), db, kr, boot, nil)
	if err != nil || !enabled {
		t.Fatalf("boot resolve: enabled=%v err=%v", enabled, err)
	}
	if _, ok := sender.(DevLog); !ok {
		t.Fatalf("want DevLog, got %T", sender)
	}

	en := true
	mode := ModeResend
	from := "闲猫 <noreply@idlicat.com>"
	key := "re_live_key"
	v, err = Update(context.Background(), db, kr, boot, Patch{Enabled: &en, Mode: &mode, From: &from, APIKey: &key})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Enabled || v.Mode != ModeResend || !v.HasAPIKey || v.Source != "settings" {
		t.Fatalf("after update: %+v", v)
	}

	enabled, sender, err = ResolveSender(context.Background(), db, kr, boot, nil)
	if err != nil || !enabled {
		t.Fatalf("settings resolve: enabled=%v err=%v", enabled, err)
	}
	rs, ok := sender.(*Resend)
	if !ok || rs.APIKey != "re_live_key" || rs.From != from {
		t.Fatalf("want Resend with saved key, got %#v", sender)
	}

	// 再次保存不带 api_key，应保留原密钥
	from2 := "闲猫 <onboarding@resend.dev>"
	v, err = Update(context.Background(), db, kr, boot, Patch{From: &from2})
	if err != nil {
		t.Fatal(err)
	}
	if !v.HasAPIKey {
		t.Fatal("api key should be kept")
	}
	_, sender, err = ResolveSender(context.Background(), db, kr, boot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sender.(*Resend).APIKey != "re_live_key" || sender.(*Resend).From != from2 {
		t.Fatalf("keep key / update from: %#v", sender)
	}
}

func TestUpdateRejectsResendWithoutKey(t *testing.T) {
	db := &memDB{}
	kr := testKeyring(t)
	en := true
	mode := ModeResend
	from := "闲猫 <a@b.c>"
	_, err := Update(context.Background(), db, kr, Boot{}, Patch{Enabled: &en, Mode: &mode, From: &from})
	if err == nil {
		t.Fatal("expected error")
	}
}
