package zalobot

import (
	"context"
	"math/rand/v2"
	"strconv"
	"time"
)

func (b *Bot) GetMe(ctx context.Context) (*BotInfo, error) {
	var info BotInfo
	if err := b.call(ctx, "getMe", map[string]any{}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (b *Bot) GetUpdates(ctx context.Context, o *GetUpdatesOptions) ([]Update, error) {
	params := map[string]any{}
	if o != nil && o.Timeout > 0 {
		params["timeout"] = strconv.Itoa(int(o.Timeout.Seconds()))
	}
	raw, err := b.api.Call(ctx, "getUpdates", params)
	if err != nil {
		return nil, err
	}
	return decodeUpdates(raw)
}

func (b *Bot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return ErrStopped
	}
	b.mu.Unlock()
	if b.polling.Load() {
		return ErrAlreadyPolling
	}
	if b.cfg.httpClient != nil && b.cfg.httpClient.Timeout > 0 && b.cfg.httpClient.Timeout <= b.cfg.pollTimeout+5*time.Second {
		return &ValidationError{Field: "httpClient.Timeout", Reason: "must exceed the polling timeout + 5s"}
	}
	if b.cfg.autoDeleteWebhook {
		if _, err := b.DeleteWebhook(ctx); err != nil {
			return err
		}
	} else {
		info, err := b.GetWebhookInfo(ctx)
		if err != nil {
			return err
		}
		if info.URL != "" {
			return &WebhookActiveError{URL: info.URL}
		}
	}

	pollCtx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		cancel()
		return ErrStopped
	}
	if b.polling.Load() {
		b.mu.Unlock()
		cancel()
		return ErrAlreadyPolling
	}
	b.pollCancel = cancel
	b.polling.Store(true)
	b.wg.Add(1)
	b.mu.Unlock()
	go b.pollLoop(pollCtx)

	go func() {
		select {
		case <-ctx.Done():
			_ = b.Stop()
		case <-b.done:
		}
	}()
	return nil
}

func (b *Bot) pollLoop(ctx context.Context) {
	defer b.wg.Done()
	defer b.polling.Store(false)
	backoff := time.Second
	empty := 0
	for {
		if b.isStopped() {
			return
		}
		pollCtx, cancel := context.WithTimeout(ctx, b.cfg.pollTimeout+5*time.Second)
		started := time.Now()
		updates, err := b.GetUpdates(pollCtx, &GetUpdatesOptions{Timeout: b.cfg.pollTimeout})
		cancel()
		if err != nil {
			if ctx.Err() != nil || b.isStopped() {
				return
			}
			if IsPollingTimeout(err) {
				empty++
				stop, attempted := b.recheckWebhook(ctx, empty)
				if attempted {
					empty = 0
				}
				if stop {
					return
				}
				sleepRemainder(ctx, b.done, started, b.cfg.pollTimeout)
				continue
			}
			if IsUnauthorized(err) {
				b.setErr(err)
				b.reportError(err)
				go func() { _ = b.Stop() }()
				return
			}
			b.reportError(err)
			if IsRateLimited(err) {
				backoff = 30 * time.Second
			} else if backoff < time.Second {
				backoff = time.Second
			}
			delay := jitter(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-b.done:
				return
			case <-time.After(delay):
			}
			continue
		}
		backoff = time.Second
		if len(updates) == 0 {
			empty++
			stop, attempted := b.recheckWebhook(ctx, empty)
			if attempted {
				empty = 0
			}
			if stop {
				return
			}
			sleepRemainder(ctx, b.done, started, b.cfg.pollTimeout)
			continue
		}
		empty = 0
		for _, u := range updates {
			if err := b.disp.admitBlocking(ctx, u); err != nil {
				return
			}
		}
	}
}

func (b *Bot) recheckWebhook(ctx context.Context, empty int) (stop, attempted bool) {
	if empty < 30 {
		return false, false
	}
	info, err := b.GetWebhookInfo(ctx)
	if err != nil {
		return false, true
	}
	if info.URL != "" {
		werr := &WebhookActiveError{URL: info.URL}
		b.setErr(werr)
		b.reportError(werr)
		go func() { _ = b.Stop() }()
		return true, true
	}
	return false, true
}

func (b *Bot) isStopped() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stopped
}

func sleepRemainder(ctx context.Context, done <-chan struct{}, started time.Time, interval time.Duration) {
	if interval > time.Second {
		interval = time.Second
	}
	if d := interval - time.Since(started); d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-done:
		case <-timer.C:
		}
	}
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}
