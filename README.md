# go-zalo-bot-api

[![CI](https://github.com/lucanhost/go-zalo-bot-api/actions/workflows/ci.yml/badge.svg)](https://github.com/lucanhost/go-zalo-bot-api/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/lucanhost/go-zalo-bot-api.svg)](https://pkg.go.dev/github.com/lucanhost/go-zalo-bot-api)
[![Go Version](https://img.shields.io/github/go-mod/go-version/lucanhost/go-zalo-bot-api)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

An idiomatic Go SDK for the [Zalo Bot Platform](https://docs.zaloplatforms.com/docs/BOT).

- **Complete**: every documented Bot API method.
- **Two update modes**: long polling and webhooks.
- **Typed**: update/message models, an error taxonomy, and status predicates.
- **Concurrency-safe**: a per-chat dispatcher that preserves order per chat
  while processing different chats in parallel.
- **No third-party runtime dependencies** — standard library only.

## Install

```bash
go get github.com/lucanhost/go-zalo-bot-api
```

Requires Go 1.25 or newer.

## Quick start (long polling)

```go
package main

import (
	"context"
	"log"

	zalobot "github.com/lucanhost/go-zalo-bot-api"
)

func main() {
	bot, err := zalobot.New("YOUR_BOT_TOKEN")
	if err != nil {
		log.Fatal(err)
	}

	bot.OnMessage(func(ctx context.Context, m *zalobot.Message) {
		if m.Text == "" {
			return
		}
		if _, err := bot.SendMessage(ctx, m.Chat.ID, "Bạn vừa nói: "+m.Text, nil); err != nil {
			log.Printf("sendMessage: %v", err)
		}
	})

	if err := bot.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	<-bot.Done()
}
```

A runnable version lives in [`examples/echo-bot`](examples/echo-bot).

## Webhooks

```go
bot, _ := zalobot.New("YOUR_BOT_TOKEN")

handler, err := bot.WebhookHandler("YOUR_SECRET_TOKEN") // 8..256 chars
if err != nil {
	log.Fatal(err)
}
http.Handle("/webhook", handler)
log.Fatal(http.ListenAndServe(":8080", nil))
```

Register the URL with Zalo:

```go
info, err := bot.SetWebhook(ctx, "https://example.com/webhook", "YOUR_SECRET_TOKEN")
if err != nil {
	log.Fatal(err)
}
if info.Verification != nil && !info.Verification.OK {
	log.Printf("webhook verification: %s (%s)", info.Verification.Outcome, info.Verification.Hint)
}
```

The handler checks the method, verifies `X-Bot-Api-Secret-Token` in constant
time, caps the body at 1 MiB, and returns `200` only after the update has been
admitted for processing (`503` if the queue is full). Handlers run
asynchronously, so a slow handler never delays the acknowledgement.

## Handlers

```go
bot.OnMessage(func(ctx context.Context, m *zalobot.Message) { /* any message */ })

bot.OnText(regexp.MustCompile(`^/echo (.+)$`), func(ctx context.Context, m *zalobot.Message, match []string) {
	bot.SendMessage(ctx, m.Chat.ID, match[1], nil)
})

bot.OnCommand("start", func(ctx context.Context, m *zalobot.Message, args []string) {
	bot.SendMessage(ctx, m.Chat.ID, "Xin chào!", nil)
})

bot.OnEvent(zalobot.EventImageReceived, func(ctx context.Context, u zalobot.Update) { /* ... */ })

bot.OnError(func(err error) { log.Printf("bot error: %v", err) })
```

Per update, handlers run in the order `OnEvent` → `OnMessage` → `OnText` →
`OnCommand`, and every matching handler runs. A panicking handler is reported
through `OnError` and does not stop the others. Prefer handlers for
correctness; `bot.Updates()` is a best-effort channel (it can drop under
backpressure).

## Rich text

Use `parse_mode` for markup, or `text_styles` for precise UTF-16 ranges:

```go
style, _ := zalobot.StyleRange("Xin chào bạn", "chào", zalobot.StyleBold, zalobot.ColorRed)
_, err := bot.SendMessage(ctx, chatID, "Xin chào bạn", &zalobot.SendMessageOptions{
	TextStyles: []zalobot.TextStyle{style},
})
```

## Sending messages

```go
bot.SendMessage(ctx, chatID, "text", &zalobot.SendMessageOptions{ParseMode: zalobot.ParseModeMarkdown})
bot.SendPhoto(ctx, chatID, "https://example.com/cat.jpg", &zalobot.SendPhotoOptions{Caption: "Mèo"})
bot.SendSticker(ctx, chatID, "0e078a2fb66a5f34067b")
bot.SendVoice(ctx, chatID, "https://example.com/audio.aac") // 1-to-1 chats only, .aac
bot.SendChatAction(ctx, chatID, zalobot.ChatActionTyping)
```

## Errors

```go
_, err := bot.SendMessage(ctx, chatID, "hi", nil)
switch {
case err == nil:
case zalobot.IsUnauthorized(err):
	log.Fatal("bot token is invalid or expired")
case zalobot.IsRateLimited(err):
	// back off and retry
case zalobot.IsPollingTimeout(err):
	// idle long poll; not an error for the polling loop
default:
	var apiErr *zalobot.APIError
	if errors.As(err, &apiErr) {
		log.Printf("api error %d: %s", apiErr.Code, apiErr.Description)
	}
}
```

## Configuration

```go
bot, err := zalobot.New(token,
	zalobot.WithPolling(zalobot.PollingOptions{Timeout: 30 * time.Second}),
	zalobot.WithWorkers(4),          // chats processed concurrently
	zalobot.WithHTTPClient(client),  // custom transport
	zalobot.WithDrainTimeout(10*time.Second),
	zalobot.WithAutoDeleteWebhook(false), // Start errors if a webhook is set
)
```

## Development

```bash
make help       # list targets
make test       # hermetic tests (no token, no network)
make test-race  # race detector + goroutine-leak check
make lint       # golangci-lint
make ci         # everything CI runs
make integration # live suite (requires ZALO_BOT_TOKEN)
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full work/test flow.

## Design

The design spec lives in
[`docs/superpowers/specs/`](docs/superpowers/specs/) and the implementation
plan in [`docs/superpowers/plans/`](docs/superpowers/plans/).

## License

[MIT](LICENSE).

## Acknowledgements

This SDK is an independent Go implementation. Its API surface was informed by
[`node-zalo-bot-api`](https://www.npmjs.com/package/node-zalo-bot) (MIT),
which in turn builds on
[`node-telegram-bot-api`](https://github.com/yagop/node-telegram-bot-api)
(MIT). No code from those projects is included here.
