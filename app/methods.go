package app

import (
	"context"
	"fmt"
	"log"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func startText(ctx context.Context, b *bot.Bot, update *models.Update) {
	send_text := "Привет!\nЯ - бот, который следит за подпиской и перенаправляет тебя в ловушку Джокера (в TG mini app).\nДавай дружить, иначе у тебя писька отвалится."

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   send_text,
	})

	if err != nil {
		log.Printf("Failed to send message: %v", err)
	}
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

func IsUserSubscribedToChannel(ctx context.Context, b *bot.Bot, userID int64) (bool, error) {
	chat, err := b.GetChat(ctx, &bot.GetChatParams{ChatID: "nothing"})
	if err != nil {
		return false, fmt.Errorf("не удалось получить информацию о канале: %w", err)
	}

	member, err := b.GetChatMember(ctx, &bot.GetChatMemberParams{
		ChatID: chat.ID,
		UserID: userID,
	})

	// Список статусов, которые считаются "подписан"
	switch member.Type {
	case models.ChatMemberTypeMember,
		models.ChatMemberTypeOwner,
		models.ChatMemberTypeAdministrator:
		return true, nil

	case models.ChatMemberTypeLeft,
		models.ChatMemberTypeBanned,
		models.ChatMemberTypeRestricted:
		return false, nil

	default:
		// Unknown / unexpected status
		return false, fmt.Errorf("неизвестный статус участника: %s", member.Type)
	}
}
