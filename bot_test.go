package zalobot

import (
	"errors"
	"testing"
)

func TestNewRejectsEmptyToken(t *testing.T) {
	if _, err := New(""); !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v, want ErrNoToken", err)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	b, err := New("TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if b.cfg.baseURL != "https://bot-api.zaloplatforms.com" {
		t.Fatalf("baseURL = %q", b.cfg.baseURL)
	}
	if b.cfg.workers != 4 || b.cfg.quantum != 1 || b.cfg.updatesBuffer != 64 {
		t.Fatalf("cfg = %+v", b.cfg)
	}
}

func TestOptionsOverride(t *testing.T) {
	b, _ := New("TOKEN", WithWorkers(8), WithQuantum(3), WithBaseURL("http://x"), WithUpdatesBuffer(7))
	if b.cfg.workers != 8 || b.cfg.quantum != 3 || b.cfg.baseURL != "http://x" || b.cfg.updatesBuffer != 7 {
		t.Fatalf("cfg = %+v", b.cfg)
	}
}

func TestNewRejectsInvalidCapacityOptions(t *testing.T) {
	for _, option := range []Option{WithWorkers(0), WithQuantum(0), WithUpdatesBuffer(0), WithPerChatBuffer(0), WithMaxBuffered(0)} {
		if _, err := New("TOKEN", option); err == nil {
			t.Fatalf("New with invalid option %T returned nil error", option)
		} else {
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %T %v, want *ValidationError", err, err)
			}
		}
	}
}
