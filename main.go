// main.go
// Telegram Bot for selling access to a private channel using Telegram Stars.
// Single-file implementation for simplicity and ease of deployment.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/lib/pq"
	_ "github.com/lib/pq" // PostgreSQL driver
)

// ============================================================================
// Configuration (loaded from environment variables)
// ============================================================================
type config struct {
	BotToken        string
	DatabaseURL     string
	ChannelID       string // Can be numeric ID (e.g., -1001234567890) or username (e.g., @mychannel)
	StarsPrice      int64  // Price in Stars
	Button1Text     string
	Button1URL      string
	Button2Text     string
	Button2URL      string
	LogFilePath     string
	AdminUserID     int64  // Optional: User ID for admin alerts (e.g., on bot start)
	WebhookURL      string // Public URL for Telegram to send updates to
	WebhookListen   string // Local address to listen on (e.g., ":8080")
	WebhookEndpoint string // Path for webhook (e.g., "/webhook")
}

func loadConfig() (*config, error) {
	cfg := &config{
		BotToken:        os.Getenv("BOT_TOKEN"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		ChannelID:       os.Getenv("PRIVATE_CHANNEL_ID"),
		Button1Text:     os.Getenv("BUTTON_1_TEXT"),
		Button1URL:      os.Getenv("BUTTON_1_URL"),
		Button2Text:     os.Getenv("BUTTON_2_TEXT"),
		Button2URL:      os.Getenv("BUTTON_2_URL"),
		LogFilePath:     os.Getenv("LOG_FILE_PATH"),
		WebhookURL:      os.Getenv("WEBHOOK_URL"),
		WebhookListen:   os.Getenv("WEBHOOK_LISTEN"),
		WebhookEndpoint: os.Getenv("WEBHOOK_ENDPOINT"),
	}

	var err error
	priceStr := os.Getenv("STARS_PRICE")
	if priceStr == "" {
		return nil, errors.New("STARS_PRICE is required")
	}
	cfg.StarsPrice, err = strconv.ParseInt(priceStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid STARS_PRICE: %w", err)
	}

	adminIDStr := os.Getenv("ADMIN_USER_ID")
	if adminIDStr != "" {
		cfg.AdminUserID, err = strconv.ParseInt(adminIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ADMIN_USER_ID: %w", err)
		}
	}

	// Basic validation
	if cfg.BotToken == "" || cfg.DatabaseURL == "" || cfg.ChannelID == "" || cfg.StarsPrice == 0 {
		return nil, errors.New("missing required environment variables (BOT_TOKEN, DATABASE_URL, PRIVATE_CHANNEL_ID, STARS_PRICE)")
	}
	if cfg.WebhookURL == "" || cfg.WebhookListen == "" || cfg.WebhookEndpoint == "" {
		return nil, errors.New("webhook configuration (WEBHOOK_URL, WEBHOOK_LISTEN, WEBHOOK_ENDPOINT) is required")
	}

	return cfg, nil
}

// ============================================================================
// Database Models and Logic
// ============================================================================

// Subscription represents a user's subscription period.
type Subscription struct {
	ID          int
	UserID      int64
	PurchasedAt time.Time
	ExpiresAt   time.Time
}

// Payment represents a payment transaction.
type Payment struct {
	ID                      int
	UserID                  int64
	TelegramPaymentChargeID string
	AmountStars             int64
	PurchasedAt             time.Time
	SubscriptionID          int // ID of the subscription this payment extended/created
}

// Database struct holds the connection pool.
type Database struct {
	db *sql.DB
}

// NewDatabase creates a new DB connection and ensures tables exist.
func NewDatabase(dataSourceName string) (*Database, error) {
	db, err := sql.Open("postgres", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Create tables if they don't exist (simple migration)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			user_id BIGINT PRIMARY KEY,
			username TEXT,
			first_name TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS subscriptions (
			id SERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
			purchased_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMPTZ NOT NULL,
			UNIQUE(user_id)
		);
		CREATE INDEX IF NOT EXISTS idx_subscriptions_expires_at ON subscriptions(expires_at);
		CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id ON subscriptions(user_id);

		CREATE TABLE IF NOT EXISTS payments (
			id SERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
			telegram_payment_charge_id TEXT UNIQUE NOT NULL,
			amount_stars BIGINT NOT NULL,
			purchased_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			subscription_id INT REFERENCES subscriptions(id) ON DELETE SET NULL
		);
		CREATE INDEX IF NOT EXISTS idx_payments_charge_id ON payments(telegram_payment_charge_id);
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return &Database{db: db}, nil
}

// Close closes the database connection.
func (d *Database) Close() error {
	return d.db.Close()
}

// ensureUser creates or updates user information.
func (d *Database) ensureUser(ctx context.Context, user *models.User) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO users (user_id, username, first_name, created_at) 
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id) DO UPDATE SET 
			username = EXCLUDED.username,
			first_name = EXCLUDED.first_name
	`, user.ID, user.Username, user.FirstName)
	return err
}

// getActiveSubscription returns the current active subscription for a user (if exists and not expired).
func (d *Database) getActiveSubscription(ctx context.Context, userID int64) (*Subscription, error) {
	var sub Subscription
	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, purchased_at, expires_at 
		FROM subscriptions 
		WHERE user_id = $1 AND expires_at > NOW() 
		ORDER BY expires_at DESC LIMIT 1
	`, userID).Scan(&sub.ID, &sub.UserID, &sub.PurchasedAt, &sub.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // No active subscription
		}
		return nil, err
	}
	return &sub, nil
}

// calculateExpiryDate calculates expiry date as midnight MSK of the day 30 days from base time
func calculateExpiryDate(baseTime time.Time, daysToAdd int) time.Time {
	loc, _ := time.LoadLocation("Europe/Moscow")
	if loc == nil {
		loc = time.FixedZone("MSK", 3*60*60)
	}
	future := baseTime.AddDate(0, 0, daysToAdd)
	// Set to midnight MSK of that day
	return time.Date(future.Year(), future.Month(), future.Day(), 0, 0, 0, 0, loc).UTC()
}

// createOrExtendSubscription creates a new subscription or extends an existing one.
// Returns the new expires_at time and the subscription ID.
func (d *Database) createOrExtendSubscription(ctx context.Context, userID int64, daysToAdd int) (time.Time, int, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, 0, err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	var newExpiresAt time.Time
	var subID int

	// Check for existing subscription and lock the row
	var currentExpiresAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT expires_at FROM subscriptions WHERE user_id = $1 FOR UPDATE`, userID).Scan(&currentExpiresAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, 0, err
	}

	if currentExpiresAt.Valid && currentExpiresAt.Time.After(now) {
		// Extend from current expiration, but align to midnight MSK
		newExpiresAt = calculateExpiryDate(currentExpiresAt.Time, daysToAdd)

		// Update existing subscription
		_, err = tx.ExecContext(ctx, `
			UPDATE subscriptions 
			SET expires_at = $1, purchased_at = NOW() 
			WHERE user_id = $2
			RETURNING id
		`, newExpiresAt, userID)
		if err != nil {
			return time.Time{}, 0, err
		}

		// Get the subscription ID
		err = tx.QueryRowContext(ctx, `SELECT id FROM subscriptions WHERE user_id = $1`, userID).Scan(&subID)
		if err != nil {
			return time.Time{}, 0, err
		}
	} else {
		// Create new subscription
		newExpiresAt = calculateExpiryDate(now, daysToAdd)

		// Insert new subscription with conflict handling
		err = tx.QueryRowContext(ctx, `
			INSERT INTO subscriptions (user_id, purchased_at, expires_at) 
			VALUES ($1, NOW(), $2)
			ON CONFLICT (user_id) DO UPDATE SET
				purchased_at = EXCLUDED.purchased_at,
				expires_at = EXCLUDED.expires_at
			RETURNING id
		`, userID, newExpiresAt).Scan(&subID)
		if err != nil {
			return time.Time{}, 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return time.Time{}, 0, err
	}
	return newExpiresAt, subID, nil
}

// recordPayment stores payment information and links it to a subscription.
func (d *Database) recordPayment(ctx context.Context, userID int64, chargeID string, amountStars int64, subscriptionID int) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO payments (user_id, telegram_payment_charge_id, amount_stars, purchased_at, subscription_id) 
		VALUES ($1, $2, $3, NOW(), $4)
	`, userID, chargeID, amountStars, subscriptionID)
	return err
}

// createSubscriptionAndRecordPayment atomically creates subscription and records payment
func (d *Database) createSubscriptionAndRecordPayment(ctx context.Context, userID int64, chargeID string, amountStars int64, daysToAdd int) (time.Time, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	var newExpiresAt time.Time
	var subID int

	// Check for existing subscription and lock the row
	var currentExpiresAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT expires_at FROM subscriptions WHERE user_id = $1 FOR UPDATE`, userID).Scan(&currentExpiresAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}

	if currentExpiresAt.Valid && currentExpiresAt.Time.After(now) {
		// Extend from current expiration
		newExpiresAt = calculateExpiryDate(currentExpiresAt.Time, daysToAdd)

		_, err = tx.ExecContext(ctx, `
			UPDATE subscriptions 
			SET expires_at = $1, purchased_at = NOW() 
			WHERE user_id = $2
		`, newExpiresAt, userID)
		if err != nil {
			return time.Time{}, err
		}

		err = tx.QueryRowContext(ctx, `SELECT id FROM subscriptions WHERE user_id = $1`, userID).Scan(&subID)
		if err != nil {
			return time.Time{}, err
		}
	} else {
		// Create new subscription
		newExpiresAt = calculateExpiryDate(now, daysToAdd)

		err = tx.QueryRowContext(ctx, `
			INSERT INTO subscriptions (user_id, purchased_at, expires_at) 
			VALUES ($1, NOW(), $2)
			ON CONFLICT (user_id) DO UPDATE SET
				purchased_at = EXCLUDED.purchased_at,
				expires_at = EXCLUDED.expires_at
			RETURNING id
		`, userID, newExpiresAt).Scan(&subID)
		if err != nil {
			return time.Time{}, err
		}
	}

	// Record payment in the same transaction
	_, err = tx.ExecContext(ctx, `
		INSERT INTO payments (user_id, telegram_payment_charge_id, amount_stars, purchased_at, subscription_id) 
		VALUES ($1, $2, $3, NOW(), $4)
	`, userID, chargeID, amountStars, subID)
	if err != nil {
		return time.Time{}, err
	}

	if err := tx.Commit(); err != nil {
		return time.Time{}, err
	}
	return newExpiresAt, nil
}

// getExpiredSubscribers returns user IDs whose subscriptions expired before today at 00:00 MSK.
func (d *Database) getExpiredSubscribers(ctx context.Context) ([]int64, error) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	if loc == nil {
		loc = time.FixedZone("MSK", 3*60*60)
	}
	// Get the start of today in MSK
	nowMSK := time.Now().In(loc)
	midnightMSK := time.Date(nowMSK.Year(), nowMSK.Month(), nowMSK.Day(), 0, 0, 0, 0, loc).UTC()

	rows, err := d.db.QueryContext(ctx, `
		SELECT user_id FROM subscriptions 
		WHERE expires_at < $1
	`, midnightMSK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, id)
	}
	return userIDs, rows.Err()
}

// deleteExpiredSubscriptions deletes expired subscriptions (for cleanup, after removal from channel).
func (d *Database) deleteExpiredSubscriptions(ctx context.Context, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	query := `DELETE FROM subscriptions WHERE user_id = ANY($1)`
	_, err := d.db.ExecContext(ctx, query, pq.Array(userIDs))
	return err
}

// ============================================================================
// Bot Handlers and Logic
// ============================================================================

type botHandler struct {
	db        *Database
	cfg       *config
	bot       *bot.Bot
	channelID string // parsed channel ID
}

// recoveryMiddleware catches panics and logs them
func recoveryMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Recovered from panic: %v", r)
				// Optionally notify admin
			}
		}()
		next(ctx, b, update)
	}
}

// newBotHandler initializes the handler and sets up the bot.
func newBotHandler(db *Database, cfg *config) (*botHandler, error) {
	handler := &botHandler{
		db:  db,
		cfg: cfg,
	}

	// Parse channel ID (could be numeric string or @username)
	channelID := cfg.ChannelID
	if !strings.HasPrefix(channelID, "-100") && !strings.HasPrefix(channelID, "@") {
		// Assume it's a public username without @
		channelID = "@" + channelID
	}
	handler.channelID = channelID

	// Initialize bot
	opts := []bot.Option{
		bot.WithDefaultHandler(handler.handleUpdate),
		// Middleware to ensure user exists in DB
		bot.WithMiddlewares(handler.ensureUserMiddleware, recoveryMiddleware),
	}
	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}
	handler.bot = b
	return handler, nil
}

// checkBotPermissions verifies that the bot has necessary rights in the channel
func (h *botHandler) checkBotPermissions(ctx context.Context) error {
	chat, err := h.bot.GetChat(ctx, &bot.GetChatParams{
		ChatID: h.channelID,
	})
	if err != nil {
		return fmt.Errorf("failed to get chat info: %w", err)
	}

	// Check if bot is admin in the channel
	if chat.Type != "channel" {
		return fmt.Errorf("chat is not a channel")
	}

	// Get bot's member info
	botMember, err := h.bot.GetChatMember(ctx, &bot.GetChatMemberParams{
		ChatID: h.channelID,
		UserID: h.bot.ID(),
	})
	if err != nil {
		return fmt.Errorf("failed to get bot's chat member info: %w", err)
	}

	// Check if bot is administrator
	if botMember.Type != "administrator" {
		return fmt.Errorf("bot must be an administrator in the channel")
	}

	log.Printf("Bot permissions verified for channel %s", h.channelID)
	return nil
}

// ensureUserMiddleware adds user to DB before any handler runs.
func (h *botHandler) ensureUserMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		var user *models.User
		if update.Message != nil {
			user = update.Message.From
		} else if update.CallbackQuery != nil {
			user = &update.CallbackQuery.From
		} else if update.PreCheckoutQuery != nil {
			user = update.PreCheckoutQuery.From
		} else if update.Message != nil && update.Message.SuccessfulPayment != nil { // Note: SuccessfulPayment is in Message
			user = update.Message.From
		}

		if user != nil {
			if err := h.db.ensureUser(ctx, user); err != nil {
				log.Printf("Failed to ensure user %d: %v", user.ID, err)
			}
		}
		next(ctx, b, update)
	}
}

// handleUpdate routes updates to specific handlers.
func (h *botHandler) handleUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	switch {
	case update.Message != nil && update.Message.Text == "/start":
		h.handleStart(ctx, b, update.Message)
	case update.Message != nil && update.Message.SuccessfulPayment != nil:
		h.handleSuccessfulPayment(ctx, b, update.Message)
	case update.PreCheckoutQuery != nil:
		h.handlePreCheckoutQuery(ctx, b, update.PreCheckoutQuery)
	case update.CallbackQuery != nil:
		h.handleCallbackQuery(ctx, b, update.CallbackQuery)
	}
}

// handleStart sends welcome message and subscription offer.
func (h *botHandler) handleStart(ctx context.Context, b *bot.Bot, message *models.Message) {
	user := message.From

	// Send welcome message (placeholder - PM will provide text)
	welcomeText := "👋 Добро пожаловать! Я помогу вам оформить доступ к приватному каналу."
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: user.ID,
		Text:   welcomeText,
	}); err != nil {
		log.Printf("Failed to send welcome to %d: %v", user.ID, err)
	}

	// Send subscription offer with inline button
	offerText := "🚀 Оформите подписку на наш приватный канал.\n\nНажмите кнопку ниже для оплаты."
	inlineKeyboard := [][]models.InlineKeyboardButton{
		{
			{
				Text:         fmt.Sprintf("Оплатить доступ %d ⭐️", h.cfg.StarsPrice),
				CallbackData: "pay", // Using callback to trigger payment creation
			},
		},
	}

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: user.ID,
		Text:   offerText,
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: inlineKeyboard,
		},
	}); err != nil {
		log.Printf("Failed to send offer to %d: %v", user.ID, err)
	}
}

// handleCallbackQuery processes button presses.
func (h *botHandler) handleCallbackQuery(ctx context.Context, b *bot.Bot, callback *models.CallbackQuery) {
	if callback.Data == "pay" {
		h.createInvoice(ctx, b, callback)
	} else if callback.Data == "show_links" {
		h.showLinks(ctx, b, callback)
	}
	// Always answer callback to remove loading state
	b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callback.ID,
	})
}

// createInvoice sends a payment invoice to the user.
func (h *botHandler) createInvoice(ctx context.Context, b *bot.Bot, callback *models.CallbackQuery) {
	user := callback.From

	// Check for active subscription to customize description
	sub, err := h.db.getActiveSubscription(ctx, user.ID)
	if err != nil {
		log.Printf("Failed to check subscription for %d: %v", user.ID, err)
		// Proceed anyway, but log error
	}

	description := "Оплатите доступ к приватному каналу на 30 дней."
	if sub != nil {
		description = fmt.Sprintf("Продлите подписку на 30 дней. Текущая подписка действует до %s.",
			sub.ExpiresAt.In(time.FixedZone("MSK", 3*60*60)).Format("02.01.2006"))
	}

	// Invoice parameters
	params := &bot.SendInvoiceParams{
		ChatID:        user.ID,
		Title:         "Подписка на канал",
		Description:   description,
		Payload:       "subscription_payload", // Arbitrary payload, can be used for idempotency
		ProviderToken: "",                     // Empty for Stars payments
		Currency:      "XTR",
		Prices: []models.LabeledPrice{
			{
				Label:  "30 дней доступа",
				Amount: 1180, // Use price from config
			},
		},
		StartParameter: "pay", // Required for deep linking, can be any string
	}

	if _, err := b.SendInvoice(ctx, params); err != nil {
		log.Printf("Failed to send invoice to %d: %v", user.ID, err)
		// Notify user
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: user.ID,
			Text:   "Извините, не удалось создать счет. Попробуйте позже.",
		})
	}
}

// handlePreCheckoutQuery confirms the payment is possible.
func (h *botHandler) handlePreCheckoutQuery(ctx context.Context, b *bot.Bot, query *models.PreCheckoutQuery) {
	// Verify that the amount matches current price
	if query.TotalAmount != 1180 {
		log.Printf("Price mismatch for user %d: expected %d, got %d",
			query.From.ID, 1180, query.TotalAmount)

		b.AnswerPreCheckoutQuery(ctx, &bot.AnswerPreCheckoutQueryParams{
			PreCheckoutQueryID: query.ID,
			OK:                 false,
			ErrorMessage:       "Цена изменилась, пожалуйста, начните оплату заново",
		})
		return
	}

	// Verify payload if needed
	if query.InvoicePayload != "subscription_payload" {
		b.AnswerPreCheckoutQuery(ctx, &bot.AnswerPreCheckoutQueryParams{
			PreCheckoutQueryID: query.ID,
			OK:                 false,
			ErrorMessage:       "Неверный параметр платежа",
		})
		return
	}

	b.AnswerPreCheckoutQuery(ctx, &bot.AnswerPreCheckoutQueryParams{
		PreCheckoutQueryID: query.ID,
		OK:                 true,
	})
}

// handleSuccessfulPayment processes a confirmed payment.
func (h *botHandler) handleSuccessfulPayment(ctx context.Context, b *bot.Bot, message *models.Message) {
	user := message.From
	payment := message.SuccessfulPayment

	log.Printf("Processing successful payment for user %d, charge ID: %s", user.ID, payment.TelegramPaymentChargeID)

	// --- Idempotency Check ---
	var exists bool
	err := h.db.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM payments WHERE telegram_payment_charge_id = $1)`, payment.TelegramPaymentChargeID).Scan(&exists)
	if err != nil {
		log.Printf("Failed to check duplicate payment %s: %v", payment.TelegramPaymentChargeID, err)
		// If we can't check, it's safer to not proceed
		return
	}
	if exists {
		log.Printf("Duplicate successful_payment received for charge ID %s, ignoring", payment.TelegramPaymentChargeID)
		return
	}

	// --- Create/Extend Subscription and Record Payment Atomically ---
	newExpiresAt, err := h.db.createSubscriptionAndRecordPayment(ctx, user.ID, payment.TelegramPaymentChargeID, (int64)(payment.TotalAmount), 30)
	if err != nil {
		log.Printf("Failed to create subscription and record payment for user %d: %v", user.ID, err)
		// Notify admin? For now, log and return.
		return
	}

	// --- Add User to Channel ---
	// Bot must be admin with "invite users" / "add members" permission.
	// Using InviteChatMembers to directly add user to channel
	_, err = b.CreateChatInviteLink(ctx, &bot.CreateChatInviteLinkParams{
		ChatID:      h.channelID,
		Name:        "Моему любимому <3",
		MemberLimit: 1,
	})
	if err != nil {
		// Check if user is already in the chat (error might be "USER_ALREADY_PARTICIPANT" or similar)
		log.Printf("Failed to add user %d to channel (might already be there): %v", user.ID, err)
	} else {
		log.Printf("Added user %d to channel %s", user.ID, h.channelID)
	}

	// --- Send Confirmation to User ---
	confirmationText := fmt.Sprintf(
		"✅ Оплата прошла успешно!\n\n"+
			"Доступ к каналу активирован до %s (МСК).\n"+
			"Если вы ещё не в канале, скоро появитесь. Спасибо за покупку!",
		newExpiresAt.In(time.FixedZone("MSK", 3*60*60)).Format("02.01.2006 15:04"),
	)
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: user.ID,
		Text:   confirmationText,
	}); err != nil {
		log.Printf("Failed to send confirmation to %d: %v", user.ID, err)
	}
}

// showLinks displays inline buttons with external links.
func (h *botHandler) showLinks(ctx context.Context, b *bot.Bot, callback *models.CallbackQuery) {
	inlineKeyboard := [][]models.InlineKeyboardButton{
		{
			{
				Text: h.cfg.Button1Text,
				URL:  h.cfg.Button1URL,
			},
		},
		{
			{
				Text: h.cfg.Button2Text,
				URL:  h.cfg.Button2URL,
			},
		},
	}

	// Edit the original message to show links, or send new one. Let's send a new one.
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: callback.From.ID,
		Text:   "Наши ресурсы:",
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: inlineKeyboard,
		},
	}); err != nil {
		log.Printf("Failed to send links to %d: %v", callback.From.ID, err)
	}
}

// ============================================================================
// Expiration Checker (Cron Job)
// ============================================================================

// startExpirationChecker runs a daily job at midnight MSK to remove expired users.
func (h *botHandler) startExpirationChecker(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour) // Check every hour to see if it's midnight MSK
	defer ticker.Stop()

	log.Println("Expiration checker started")
	h.runExpirationCheck(ctx) // Run once immediately on start

	for {
		select {
		case <-ctx.Done():
			log.Println("Expiration checker stopped")
			return
		case t := <-ticker.C:
			loc, _ := time.LoadLocation("Europe/Moscow")
			if loc == nil {
				loc = time.FixedZone("MSK", 3*60*60)
			}
			nowMSK := t.In(loc)
			// Check if it's between 00:00 and 00:01 MSK (to avoid multiple runs)
			if nowMSK.Hour() == 0 && nowMSK.Minute() == 0 {
				h.runExpirationCheck(ctx)
			}
		}
	}
}

// runExpirationCheck performs the actual removal of expired users.
func (h *botHandler) runExpirationCheck(ctx context.Context) {
	log.Println("Running daily expiration check...")

	expiredIDs, err := h.db.getExpiredSubscribers(ctx)
	if err != nil {
		log.Printf("Failed to get expired subscribers: %v", err)
		return
	}

	if len(expiredIDs) == 0 {
		log.Println("No expired users found")
		return
	}

	log.Printf("Found %d expired users to remove from channel", len(expiredIDs))

	// For each expired user: remove from channel, notify, then delete subscription record
	for _, userID := range expiredIDs {
		// Remove from channel using UnbanChatMember (removes user without banning)
		_, err := h.bot.UnbanChatMember(ctx, &bot.UnbanChatMemberParams{
			ChatID:       h.channelID,
			UserID:       userID,
			OnlyIfBanned: false, // This will remove the user even if not banned
		})
		if err != nil {
			log.Printf("Failed to remove user %d from channel: %v", userID, err)
			// Continue to next user, maybe log for manual check
			continue
		}

		log.Printf("Removed user %d from channel", userID)

		// Send notification
		notificationText := "❌ Срок вашей подписки истек. Чтобы продолжить доступ, оформите подписку заново через команду /start."
		if _, err := h.bot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: userID,
			Text:   notificationText,
		}); err != nil {
			log.Printf("Failed to send expiration notification to %d: %v", userID, err)
		}
	}

	// Optionally delete all expired subscription records in one go
	if err := h.db.deleteExpiredSubscriptions(ctx, expiredIDs); err != nil {
		log.Printf("Failed to delete expired subscription records: %v", err)
	}

	log.Printf("Expiration check completed. Processed %d users.", len(expiredIDs))
}

// ============================================================================
// Main Function
// ============================================================================

func main() {
	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	// Setup logging to file
	logFile, err := os.OpenFile(cfg.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("=== Bot starting ===")

	// Connect to database
	db, err := NewDatabase(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer db.Close()
	log.Println("Database connected")

	// Create handler and bot
	handler, err := newBotHandler(db, cfg)
	if err != nil {
		log.Fatalf("Failed to initialize bot handler: %v", err)
	}
	log.Println("Bot handler created")

	// Check bot permissions in channel
	if err := handler.checkBotPermissions(ctxWithTimeout()); err != nil {
		log.Fatalf("Bot permission check failed: %v", err)
	}
	log.Println("Bot permissions verified")

	// Set webhook
	webhookURL := fmt.Sprintf("%s%s", strings.TrimRight(cfg.WebhookURL, "/"), cfg.WebhookEndpoint)
	if _, err := handler.bot.SetWebhook(ctxWithTimeout(), &bot.SetWebhookParams{
		URL: webhookURL,
	}); err != nil {
		log.Fatalf("Failed to set webhook: %v", err)
	}
	log.Printf("Webhook set to %s", webhookURL)

	// Start expiration checker in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go handler.startExpirationChecker(ctx)

	// Start webhook server
	mux := http.NewServeMux()
	mux.HandleFunc(cfg.WebhookEndpoint, handler.bot.WebhookHandler())

	server := &http.Server{
		Addr:    cfg.WebhookListen,
		Handler: mux,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down...")
		cancel() // Stop expiration checker

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	log.Printf("Starting webhook server on %s", cfg.WebhookListen)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("HTTP server error: %v", err)
	}

	log.Println("Bot stopped")
}

func ctxWithTimeout() context.Context {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second) //nolint:govet
	return ctx
}
