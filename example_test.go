package zalobot_test

import (
	"context"
	"fmt"
	"log"
	"regexp"

	zalobot "github.com/lucanhost/go-zalo-bot-api"
)

func ExampleBot_OnText() {
	bot, err := zalobot.New("YOUR_BOT_TOKEN")
	if err != nil {
		log.Fatal(err)
	}
	bot.OnText(regexp.MustCompile(`^/echo (.+)$`), func(ctx context.Context, m *zalobot.Message, match []string) {
		_, _ = bot.SendMessage(ctx, m.Chat.ID, match[1], nil)
	})
	fmt.Println("handler registered")
	// Output: handler registered
}

func ExampleStyleRange() {
	ts, err := zalobot.StyleRange("Xin chào bạn", "chào", zalobot.StyleBold, zalobot.ColorRed)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("start=%d len=%d\n", ts.Start, ts.Len)
	// Output: start=4 len=4
}
