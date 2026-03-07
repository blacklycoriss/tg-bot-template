package app

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
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
	b, err := bot.New("TOKEN_BOT", opts...)
	if err != nil {
		log.Panicf("Can't init bot with error %s", err)
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, startHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/check", bot.MatchTypeExact, checkSubscriptionHandler)

	log.Println("Start Bot")
	//Bot start
	b.Start(ctx)
}

//-----------------------------------------------------------------------------------------------------

//METHODS

func startHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	send_text := "Привет!\nЯ - бот, который следит за подпиской и перенаправляет тебя в ловушку Джокера (в TG mini app).\nДавай дружить, иначе у тебя писька отвалится."

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   send_text,
	})

	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID, // ← ваш chat ID (или -100XXXXXXXX для канала)
		Text:   "Подпишись, Солнышко 🥺",
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{
						Text: "Подписаться ❤️",
						URL:  "nothing",
					},
				},
			},
		},
	})

	if err != nil {
		log.Printf("Failed to send message: %v", err)
	}
}

func checkSubscriptionHandler(ctx context.Context, b *bot.Bot, update *models.Update) {

}

func showMessageWithBot(next bot.HandlerFunc) bot.HandlerFunc {

	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message != nil {
			log.Printf("Bot say: %s", update.Message.Text)
		}
		next(ctx, b, update)
	}
}

func showMessageWithUser(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message != nil {
			log.Printf("%s (%d) say: %s", update.Message.From.FirstName, update.Message.From.ID, update.Message.Text)
		}
		next(ctx, b, update)
	}
}
