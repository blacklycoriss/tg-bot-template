package app

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-telegram/bot"
)

var (
	BOT_TOKEN          = "8660716796:AAEE1fJz9_NMEfodNkCmN9SBdMtpxObGvGY"
	CHANNEL_ID   int64 = -1002172145911
	CHANNEL_LINK       = "https://t.me/+uYoqgE4rQfY0Y2Ji"
)

func Start() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	opts := []bot.Option{
		bot.WithMiddlewares(checkSubscriptionMiddleware),
	}

	b, err := bot.New(BOT_TOKEN, opts...)
	if err != nil {
		log.Fatalf("Не удалось создать бота: %v", err)
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, startHandler)

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "check_sub", bot.MatchTypeExact, checkSubscriptionHandler)

	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, openTMA)

	log.Println("Бот запущен")
	b.Start(ctx)
}
