# go-zalo-bot-api

Idiomatic Go SDK for the [Zalo Bot Platform](https://docs.zaloplatforms.com/docs/BOT).

## Install

```bash
go get github.com/lucanhost/go-zalo-bot-api
```

## Polling

```go
package main

import (
	"context"
	"log"
	"time"

	zalobot "github.com/lucanhost/go-zalo-bot-api"
)

func main() {
	bot, err := zalobot.New("YOUR_BOT_TOKEN", zalobot.WithPolling(zalobot.PollingOptions{Timeout: 30 * time.Second}))
	if err != nil {
		log.Fatal(err)
	}
	bot.OnMessage(func(ctx context.Context, m *zalobot.Message) {
		_, _ = bot.SendMessage(ctx, m.Chat.ID, "Xin chào!", nil)
	})
	if err := bot.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	<-bot.Done()
}
```

## Webhook

```go
handler, err := bot.WebhookHandler("YOUR_SECRET_TOKEN")
if err != nil {
	log.Fatal(err)
}
http.Handle("/webhook", handler)
log.Fatal(http.ListenAndServe(":8080", nil))
```

## Rich text

```go
style, _ := zalobot.StyleRange("Xin chào bạn", "chào", zalobot.StyleBold, zalobot.ColorRed)
_, _ = bot.SendMessage(ctx, chatID, "Xin chào bạn", &zalobot.SendMessageOptions{
	TextStyles: []zalobot.TextStyle{style},
})
```

See `docs/superpowers/specs/2026-09-24-go-zalo-bot-api-design.md` for the full design.
