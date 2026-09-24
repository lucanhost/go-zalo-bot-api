// Command echo-bot is a minimal Zalo bot that echoes text messages back to
// the sender using long polling.
//
// Usage:
//
//	export ZALO_BOT_TOKEN=your_bot_token
//	go run ./examples/echo-bot
package main

import (
	"context"
	"log"
	"os"
	"time"

	zalobot "github.com/lucanhost/go-zalo-bot-api"
)

func main() {
	token := os.Getenv("ZALO_BOT_TOKEN")
	if token == "" {
		log.Fatal("ZALO_BOT_TOKEN is required")
	}

	bot, err := zalobot.New(token, zalobot.WithPolling(zalobot.PollingOptions{Timeout: 30 * time.Second}))
	if err != nil {
		log.Fatal(err)
	}

	bot.OnError(func(err error) { log.Printf("bot error: %v", err) })
	bot.OnMessage(func(ctx context.Context, m *zalobot.Message) {
		if m.Text == "" {
			return
		}
		if _, err := bot.SendMessage(ctx, m.Chat.ID, "Bạn vừa nói: "+m.Text, nil); err != nil {
			log.Printf("sendMessage: %v", err)
		}
	})

	log.Println("bot started; press Ctrl+C to stop")
	if err := bot.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	<-bot.Done()
}
