package app

import (
	"context"
	"fmt"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func startHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	userFirstName := update.Message.From.FirstName

	if userFirstName == "" {
		userFirstName = "путешественник"
	}

	text := fmt.Sprintf("Привет, %s! 👋\n\nЧтобы продолжить общение, пожалуйста, подпишись на наш канал:", userFirstName)

	sendWithSubscribeButton(ctx, b, chatID, text)
}
func checkSubscriptionHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	query := update.CallbackQuery
	userID := query.From.ID
	chatID := query.Message.Message.Chat.ID

	b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
	})

	isMember, err := isUserChannelMember(ctx, b, userID)
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Не удалось проверить подписку. Попробуй позже.",
		})
		return
	}

	if isMember {
		thankYou(ctx, b, chatID)

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Теперь можем общаться! 😊\nНапиши что-нибудь, я отвечу.",
		})
	} else {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            "Пока не вижу подписку 😔\nПодпишись и попробуй ещё раз",
			ShowAlert:       true,
		})
	}
}
func echoHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	text := update.Message.Text
	if text == "" {
		text = "…"
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Ты написал: " + text,
	})
}
func isUserChannelMember(ctx context.Context, b *bot.Bot, userID int64) (bool, error) {
	member, err := b.GetChatMember(ctx, &bot.GetChatMemberParams{
		ChatID: CHANNEL_ID,
		UserID: userID,
	})
	if err != nil {
		return false, err
	}

	switch member.Type {
	case models.ChatMemberTypeMember,
		models.ChatMemberTypeOwner,
		models.ChatMemberTypeAdministrator:
		return true, nil
	default:
		return false, nil
	}
}
func sendWithSubscribeButton(ctx context.Context, b *bot.Bot, chatID any, text string) {
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "Подписаться ❤️", URL: CHANNEL_LINK},
				},
				{
					{Text: "Я подписался → проверить", CallbackData: "check_sub"},
				},
			},
		},
		ParseMode: models.ParseModeHTML,
	})
}
func offerToSubscribe(ctx context.Context, b *bot.Bot, chatID any) {
	text := "Чтобы продолжить, подпишись на канал 👇\n\nПосле подписки нажми кнопку ниже:"
	sendWithSubscribeButton(ctx, b, chatID, text)
}

func thankYou(ctx context.Context, b *bot.Bot, chatID any) {
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      "Спасибо за подписку! 🎉\nТеперь можем общаться без ограничений.",
		ParseMode: models.ParseModeHTML,
	})
}
