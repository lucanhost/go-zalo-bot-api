package zalobot

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/lucanhost/go-zalo-bot-api/internal/api"
)

type handlerCtxKey struct{}

type Bot struct {
	token string
	cfg   *config
	api   *api.Client
	disp  *dispatcher

	handlers *handlerSet

	mu         sync.Mutex
	err        error
	stopped    bool
	polling    atomic.Bool
	pollCancel context.CancelFunc

	handlerCtx    context.Context
	handlerCancel context.CancelFunc

	updatesCh chan Update
	updatesOn atomic.Bool

	wg        sync.WaitGroup
	done      chan struct{}
	closeOnce sync.Once
}

func New(token string, opts ...Option) (*Bot, error) {
	if token == "" {
		return nil, ErrNoToken
	}
	cfg := defaultConfig()
	for _, o := range opts {
		if o != nil {
			o(cfg)
		}
	}
	if cfg.workers < 1 || cfg.quantum < 1 || cfg.updatesBuffer < 1 || cfg.perChatBuffer < 1 || cfg.maxBuffered < 1 {
		return nil, &ValidationError{Field: "options", Reason: "workers, quantum, updates buffer, per-chat buffer, and max buffered must be at least 1"}
	}
	b := &Bot{
		token:     token,
		cfg:       cfg,
		api:       api.NewClient(token, cfg.baseURL, cfg.httpClient),
		handlers:  newHandlerSet(),
		updatesCh: make(chan Update, cfg.updatesBuffer),
		done:      make(chan struct{}),
	}
	hctx, hcancel := context.WithCancel(context.Background())
	b.handlerCtx = context.WithValue(hctx, handlerCtxKey{}, true)
	b.handlerCancel = hcancel

	b.disp = newDispatcher(dispatchConfig{
		workers:     cfg.workers,
		quantum:     cfg.quantum,
		perChat:     cfg.perChatBuffer,
		maxBuffered: cfg.maxBuffered,
	}, b.updatesCh, &b.updatesOn, b.reportError, func(u Update) {
		b.handlers.run(b.handlerCtx, u)
	})
	b.handlers.onPanic = b.reportError
	return b, nil
}

func (b *Bot) reportError(err error) {
	if err == nil {
		return
	}
	if len(b.handlers.errorHandlers()) == 0 {
		slog.Default().Error("zalobot", "error", err)
		return
	}
	for _, h := range b.handlers.errorHandlers() {
		func() {
			defer func() { _ = recover() }()
			h(err)
		}()
	}
}

func (b *Bot) Done() <-chan struct{} { return b.done }

func (b *Bot) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

func (b *Bot) setErr(err error) {
	b.mu.Lock()
	if b.err == nil {
		b.err = err
	}
	b.mu.Unlock()
}

func (b *Bot) IsPolling() bool { return b.polling.Load() }
