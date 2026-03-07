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
		bot.WithMiddlewares(showMessageWithUserID, showMessageWithUserName),
		//Add default handler (handle every user message)
		bot.WithDefaultHandler(handler),
	}
	log.Println("Created bot options")

	//Bot init
	b, err := bot.New("YOUR_BOT_TOKEN_FROM_BOTFATHER", opts...)
	if err != nil {
		log.Panicf("Can't init bot with error %s", err)
	}

	log.Println("Start Bot")
	//Bot start
	b.Start(ctx)
}

//-----------------------------------------------------------------------------------------------------

//METHODS

// Handler-method
func handler(ctx context.Context, b *bot.Bot, update *models.Update) {
	user_text := update.Message.Text
	send_text := ""

	switch user_text {
	case "/start":
		send_text = "Привет!\nЯ - бот, который следит за подпиской и перенаправляет тебя в ловушку Джокера (в TG mini app).\nДавай дружить, иначе у тебя писька отвалится."
	default:
		send_text = "Не понимаю тебя\n"
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   send_text,
	})
	log.Printf("Bot say: %s", send_text)

	if user_text == "/start" {

		_, err := b.SendMessage(ctx, &bot.SendMessageParams{
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
		log.Printf("Bot say: %s", send_text)
	}
}

func showMessageWithUserID(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message != nil {
			log.Printf("%d say: %s", update.Message.From.ID, update.Message.Text)
		}
		next(ctx, b, update)
	}
}

func showMessageWithUserName(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message != nil {
			log.Printf("%s say: %s", update.Message.From.FirstName, update.Message.Text)
		}
		next(ctx, b, update)
	}
}
