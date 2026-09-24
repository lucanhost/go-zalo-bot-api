package zalobot

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/lucanhost/go-zalo-bot-api/internal/api"
)

type handlerCtxKey struct{}

// Bot is a client for a single Zalo bot. It is safe for concurrent use.
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

// New creates a Bot for the given token. It returns ErrNoToken when the token
// is empty and a ValidationError when an option value is out of range.
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

// Done returns a channel that is closed once the bot has fully stopped, after
// either Stop/Shutdown or a fatal polling error.
func (b *Bot) Done() <-chan struct{} { return b.done }

// Err returns the error that stopped the bot, or nil after a graceful stop.
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

// IsPolling reports whether the long-polling loop is running.
func (b *Bot) IsPolling() bool { return b.polling.Load() }

// Shutdown stops the bot and waits for the drain or ctx deadline. If shutdown is
// already in progress, it returns nil immediately; callers that need to wait for
// completion must wait on Done().
func (b *Bot) Shutdown(ctx context.Context) error {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return nil
	}
	b.stopped = true
	cancel := b.pollCancel
	b.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	b.disp.stop()

	if ctx.Value(handlerCtxKey{}) != nil {
		go func() {
			dctx, dcancel := context.WithTimeout(context.Background(), b.cfg.drainTimeout)
			defer dcancel()
			_ = b.finishShutdown(dctx)
		}()
		return nil
	}
	return b.finishShutdown(ctx)
}

// Stop initiates shutdown with the configured drain timeout. If shutdown is
// already in progress, it returns nil immediately; callers that need to wait for
// completion must wait on Done().
func (b *Bot) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), b.cfg.drainTimeout)
	defer cancel()
	return b.Shutdown(ctx)
}

func (b *Bot) finishShutdown(ctx context.Context) error {
	waited := make(chan struct{})
	go func() {
		b.wg.Wait()
		b.disp.wait()
		close(waited)
	}()
	select {
	case <-waited:
		b.handlerCancel()
		b.closeSignals()
		return nil
	case <-ctx.Done():
		b.handlerCancel()
		go func() {
			<-waited
			b.closeSignals()
		}()
		return ctx.Err()
	}
}

func (b *Bot) closeSignals() {
	b.closeOnce.Do(func() {
		close(b.updatesCh)
		close(b.done)
	})
}
