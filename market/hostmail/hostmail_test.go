package hostmail

import (
	"context"
	"errors"
	"testing"
)

func TestBridgeSendAndStatus(t *testing.T) {
	t.Cleanup(func() { SetBridge(Bridge{}) })
	SetBridge(Bridge{})
	if Active() {
		t.Fatal("empty bridge should clear")
	}
	if _, err := LoadStatus(context.Background()); !errors.Is(err, ErrNoBridge) {
		t.Fatalf("want ErrNoBridge, got %v", err)
	}

	var sentTo string
	SetBridge(Bridge{
		Status: func(context.Context) (Status, error) {
			return Status{VerifyEnabled: true, SMTPReady: true, From: "闲猫 <a@b.c>"}, nil
		},
		Send: func(_ context.Context, to, _, _ string) error {
			sentTo = to
			return nil
		},
	})
	if !Active() {
		t.Fatal("expected active")
	}
	st, err := LoadStatus(context.Background())
	if err != nil || !st.VerifyEnabled || !st.SMTPReady {
		t.Fatalf("%+v %v", st, err)
	}
	if err := (Sender{}).Send(context.Background(), "u@e.com", "s", "<b>1</b>"); err != nil {
		t.Fatal(err)
	}
	if sentTo != "u@e.com" {
		t.Fatalf("sentTo=%q", sentTo)
	}
}
