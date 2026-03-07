package app

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/go-telegram/bot"
)

func Start() {
	//Create log file
	file, err := os.OpenFile("logs/app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}
	log.SetOutput(file)
	defer file.Close()

	log.Println("Main started")

	//Add context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	//Set Bot options
	opts := []bot.Option{
		//Add Middlewares (log user input)
		bot.WithMiddlewares(showMessageWithBot, showMessageWithUser),
	}
	log.Println("Created bot options")

	//Bot init
	b, err := bot.New("BOT_TOKEN", opts...)
	if err != nil {
		log.Panicf("Can't init bot with error %s", err)
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, startHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/check", bot.MatchTypeExact, checkSubscriptionHandler)

	log.Println("Start Bot")
	//Bot start
	b.Start(ctx)
}
