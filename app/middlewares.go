package app

import (
	"context"
	"log"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func checkSubscriptionMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil {
			next(ctx, b, update)
			return
		}

		userID := update.Message.From.ID

		isMember, err := isUserChannelMember(ctx, b, userID)
		if err != nil {
			log.Printf("Ошибка проверки подписки user=%d: %v", userID, err)
			next(ctx, b, update)
			return
		}

		if isMember == false {
			offerToSubscribe(ctx, b, update.Message.Chat.ID)
		} else {
			next(ctx, b, update)
		}
	}
}
