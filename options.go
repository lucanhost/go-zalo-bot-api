package zalobot

import (
	"net/http"
	"time"
)

// Option configures a Bot created by New.
type Option func(*config)

type config struct {
	httpClient        *http.Client
	baseURL           string
	pollTimeout       time.Duration
	workers           int
	quantum           int
	updatesBuffer     int
	perChatBuffer     int
	maxBuffered       int
	drainTimeout      time.Duration
	autoDeleteWebhook bool
}

func defaultConfig() *config {
	return &config{
		httpClient:    http.DefaultClient,
		baseURL:       "https://bot-api.zaloplatforms.com",
		pollTimeout:   30 * time.Second,
		workers:       4,
		quantum:       1,
		updatesBuffer: 64,
		perChatBuffer: 512,
		maxBuffered:   10000,
		drainTimeout:  10 * time.Second,
	}
}

// WithHTTPClient sets the HTTP client used for API calls.
func WithHTTPClient(c *http.Client) Option { return func(cfg *config) { cfg.httpClient = c } }

// WithBaseURL overrides the API base URL. It is mainly useful for tests and
// proxies.
func WithBaseURL(u string) Option { return func(cfg *config) { cfg.baseURL = u } }

// WithWorkers sets how many chats are processed concurrently. Values below 1
// are rejected by New.
func WithWorkers(n int) Option { return func(cfg *config) { cfg.workers = n } }

// WithQuantum sets how many updates a worker processes from one chat before
// that chat is requeued at the tail of the ready queue.
func WithQuantum(n int) Option { return func(cfg *config) { cfg.quantum = n } }

// WithUpdatesBuffer sets the capacity of the Updates channel.
func WithUpdatesBuffer(n int) Option { return func(cfg *config) { cfg.updatesBuffer = n } }

// WithPerChatBuffer sets the maximum number of queued updates per chat.
func WithPerChatBuffer(n int) Option { return func(cfg *config) { cfg.perChatBuffer = n } }

// WithMaxBuffered sets the global ceiling on queued updates across all chats.
func WithMaxBuffered(n int) Option { return func(cfg *config) { cfg.maxBuffered = n } }

// WithDrainTimeout bounds how long Stop and Shutdown wait for in-flight
// handlers before cancelling them.
func WithDrainTimeout(d time.Duration) Option { return func(cfg *config) { cfg.drainTimeout = d } }

// WithAutoDeleteWebhook makes Start delete an existing webhook instead of
// returning a WebhookActiveError. It is off by default so a development bot
// cannot silently disable a production webhook.
func WithAutoDeleteWebhook(b bool) Option { return func(cfg *config) { cfg.autoDeleteWebhook = b } }

// PollingOptions configures long polling.
type PollingOptions struct{ Timeout time.Duration }

// WithPolling sets the long-poll timeout. A non-positive Timeout leaves the
// default (30s) in place.
func WithPolling(o PollingOptions) Option {
	return func(cfg *config) {
		if o.Timeout > 0 {
			cfg.pollTimeout = o.Timeout
		}
	}
}

// SendMessageOptions configures SendMessage. ParseMode and TextStyles are
// mutually exclusive.
type SendMessageOptions struct {
	ParseMode  ParseMode
	TextStyles []TextStyle
}

// SendPhotoOptions configures SendPhoto.
type SendPhotoOptions struct{ Caption string }

// GetUpdatesOptions configures GetUpdates.
type GetUpdatesOptions struct{ Timeout time.Duration }
