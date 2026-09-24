package zalobot

import (
	"net/http"
	"time"
)

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

func WithHTTPClient(c *http.Client) Option    { return func(cfg *config) { cfg.httpClient = c } }
func WithBaseURL(u string) Option             { return func(cfg *config) { cfg.baseURL = u } }
func WithWorkers(n int) Option                { return func(cfg *config) { cfg.workers = n } }
func WithQuantum(n int) Option                { return func(cfg *config) { cfg.quantum = n } }
func WithUpdatesBuffer(n int) Option          { return func(cfg *config) { cfg.updatesBuffer = n } }
func WithPerChatBuffer(n int) Option          { return func(cfg *config) { cfg.perChatBuffer = n } }
func WithMaxBuffered(n int) Option            { return func(cfg *config) { cfg.maxBuffered = n } }
func WithDrainTimeout(d time.Duration) Option { return func(cfg *config) { cfg.drainTimeout = d } }
func WithAutoDeleteWebhook(b bool) Option     { return func(cfg *config) { cfg.autoDeleteWebhook = b } }

type PollingOptions struct{ Timeout time.Duration }

func WithPolling(o PollingOptions) Option {
	return func(cfg *config) {
		if o.Timeout > 0 {
			cfg.pollTimeout = o.Timeout
		}
	}
}

type SendMessageOptions struct {
	ParseMode  ParseMode
	TextStyles []TextStyle
}

type SendPhotoOptions struct{ Caption string }

type GetUpdatesOptions struct{ Timeout time.Duration }
