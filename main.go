package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func main() {
	log.Println("Main started")

	//Add context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	//Add default handler
	opts := []bot.Option{
		bot.WithDefaultHandler(handler),
	}
	log.Println("Create bot options")

	//Bot init
	b, err := bot.New("YOUR_BOT_TOKEN_FROM_BOTFATHER", opts...)
	if err != nil {
		log.Panicf("Can't create bot with error %s", err)
	}
	//Bot start
	b.Start(ctx)
	log.Println("Bot started")
}

// Handler-method
func handler(ctx context.Context, b *bot.Bot, update *models.Update) {
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   update.Message.Text,
	})
}
