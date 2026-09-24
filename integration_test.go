//go:build integration

package zalobot

import (
	"context"
	"os"
	"testing"
	"time"
)

func liveBot(t *testing.T) *Bot {
	t.Helper()
	token := os.Getenv("ZALO_BOT_TOKEN")
	if token == "" {
		t.Skip("ZALO_BOT_TOKEN not set")
	}
	b, err := New(token)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLiveGetMe(t *testing.T) {
	b := liveBot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := b.GetMe(ctx)
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if info.ID == "" {
		t.Fatalf("empty bot info: %+v", info)
	}
}

func TestLiveGetUpdatesShape(t *testing.T) {
	b := liveBot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	ups, err := b.GetUpdates(ctx, &GetUpdatesOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	t.Logf("getUpdates returned %d update(s)", len(ups))
}
