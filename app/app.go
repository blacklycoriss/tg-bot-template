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
	BOT_TOKEN          = "BOT_TOKEN"
	CHANNEL_ID   int64 = 0
	CHANNEL_LINK       = "NOTHING"
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

	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, echoHandler)

	log.Println("Бот запущен")
	b.Start(ctx)
}
