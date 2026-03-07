package app

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	// Текстовые сообщения для разных статусов
	MsgSubscribed    = "✅ Вы подписаны на канал — добро пожаловать!"
	MsgNotSubscribed = "❌ Вы не подписаны на канал. Подпишитесь пожалуйста:\n\nhttps://t.me/+ваша_приватная_ссылка_или_@username"
	MsgError         = "⚠️ Не удалось проверить подписку. Попробуйте позже."
)

func startHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	startText(ctx, b, update)

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	// Можно сразу проверять подписку при /start
	isSubscribed, err := IsUserSubscribedToChannel(ctx, b, userID)

	var text string
	var replyMarkup models.ReplyMarkup

	if err != nil {
		text = MsgError + "\n\n(техническая ошибка: " + err.Error() + ")"
	} else if isSubscribed {
		text = MsgSubscribed
	} else {
		text = MsgNotSubscribed
		replyMarkup = &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{
						Text: "Подписаться ❤️",
						URL:  "nothing", // или приватная ссылка https://t.me/+
					},
				},
				{
					{
						Text:         "Я подписался → проверить",
						CallbackData: "check_subscription",
					},
				},
			},
		}
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: replyMarkup,
		ParseMode:   models.ParseModeHTML,
	})
}

func checkSubscriptionHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	isSubscribed, err := IsUserSubscribedToChannel(ctx, b, userID)

	text := ""
	if err != nil {
		text = "Ошибка проверки: " + err.Error()
	} else if isSubscribed {
		text = "Вы **подписаны** на канал ✓"
	} else {
		text = "Вы **не подписаны** на канал ✗\nПодпишитесь и попробуйте снова."
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: models.ParseModeMarkdown,
	})
}
