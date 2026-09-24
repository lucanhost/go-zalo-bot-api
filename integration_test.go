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
		t.Fatalf("empty bot id: %+v", info)
	}
	if info.AccountName == "" {
		t.Errorf("AccountName is empty: %+v", info)
	}
	t.Logf("bot id=%s account_name=%q display_name=%q account_type=%q can_join_groups=%v",
		info.ID, info.AccountName, info.DisplayName, info.AccountType, info.CanJoinGroups)
}

func TestLiveGetWebhookInfo(t *testing.T) {
	b := liveBot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := b.GetWebhookInfo(ctx)
	if err != nil {
		t.Fatalf("GetWebhookInfo: %v", err)
	}
	// An empty URL means no webhook is registered; Start treats "" as "none".
	t.Logf("webhook url=%q updated_at=%d", info.URL, info.UpdatedAt)
}

func TestLiveGetUpdatesShape(t *testing.T) {
	b := liveBot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	ups, err := b.GetUpdates(ctx, &GetUpdatesOptions{Timeout: 30 * time.Second})
	if err != nil {
		if IsPollingTimeout(err) {
			t.Logf("idle poll returned the documented 408 timeout (no updates)")
			return
		}
		t.Fatalf("GetUpdates: %v", err)
	}
	t.Logf("getUpdates returned %d update(s)", len(ups))
}

func TestLiveGetUpdatesIdle408(t *testing.T) {
	b := liveBot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ups, err := b.GetUpdates(ctx, &GetUpdatesOptions{Timeout: time.Second})
	if err != nil {
		if !IsPollingTimeout(err) {
			t.Fatalf("GetUpdates idle: %v, want 408 polling timeout", err)
		}
		t.Logf("idle getUpdates returned 408 as expected: %v", err)
		return
	}
	// An update arrived within the timeout; that is also valid.
	t.Logf("getUpdates returned %d update(s) before the idle timeout", len(ups))
}
