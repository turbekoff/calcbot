package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	ErrClosed         = errors.New("bot has closed")
	ErrSessionExpired = errors.New("session has expired")
	ErrAlreadyStarted = errors.New("bot already started")
)

var botKeyboard = tgbotapi.NewInlineKeyboardMarkup(
	tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("AC", "AC"),
		tgbotapi.NewInlineKeyboardButtonData("C", "C"),
		tgbotapi.NewInlineKeyboardButtonData("%", "%"),
		tgbotapi.NewInlineKeyboardButtonData("÷", "/"),
	),
	tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("7", "7"),
		tgbotapi.NewInlineKeyboardButtonData("8", "8"),
		tgbotapi.NewInlineKeyboardButtonData("9", "9"),
		tgbotapi.NewInlineKeyboardButtonData("×", "*"),
	),
	tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("4", "4"),
		tgbotapi.NewInlineKeyboardButtonData("5", "5"),
		tgbotapi.NewInlineKeyboardButtonData("6", "6"),
		tgbotapi.NewInlineKeyboardButtonData("-", "-"),
	),
	tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("1", "1"),
		tgbotapi.NewInlineKeyboardButtonData("2", "2"),
		tgbotapi.NewInlineKeyboardButtonData("3", "3"),
		tgbotapi.NewInlineKeyboardButtonData("+", "+"),
	),
	tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("±", "T"),
		tgbotapi.NewInlineKeyboardButtonData("0", "0"),
		tgbotapi.NewInlineKeyboardButtonData(".", "."),
		tgbotapi.NewInlineKeyboardButtonData("=", "="),
	),
)

type Bot struct {
	mc         *Memcached
	api        *tgbotapi.BotAPI
	config     *Config
	welcome    string
	help       string
	isStarted  atomic.Bool
	inShutdown atomic.Bool
	isDone     chan struct{}
	logger     *log.Logger
}

func LoadBot(config *Config, logger *log.Logger) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(config.BotToken)
	if err != nil {
		return nil, err
	}

	return &Bot{
		api:    api,
		config: config,
		logger: logger,
		isDone: make(chan struct{}),
		mc: NewMemcached(
			config.MemcachedTTLTimeout,
			config.MemcachedCleanupTimeout,
		),
		welcome: fmt.Sprintf(
			"%s%s %s of inactivity.",
			"Welcome! Type /open to get started.\n",
			"Note: the session expires after",
			config.MemcachedTTLTimeout,
		),
		help: strings.Join([]string{
			"Help:",
			"/start - welcome message.",
			"/open - open new session.",
			"/help - send this message.",
		}, "\n"),
	}, nil
}

func (b *Bot) Run() error {
	if b.isStarted.Load() {
		return ErrAlreadyStarted
	}
	b.isStarted.Store(true)
	defer close(b.isDone)

	updateConfig := tgbotapi.NewUpdate(b.config.BotOffset)
	updateConfig.Timeout = b.config.BotTimeout
	updates := b.api.GetUpdatesChan(updateConfig)

	for update := range updates {
		if b.inShutdown.Load() && b.mc.IsEmpty() {
			continue
		}

		if update.CallbackQuery != nil {
			if err := b.handleCallback(update.CallbackQuery); err != nil {
				b.logger.Printf("failed to send message, error: %v", err)
				continue
			}
		}

		if update.Message == nil {
			continue
		}

		if err := b.handleCommand(update.Message); err != nil {
			b.logger.Printf("failed to send message, error: %v", err)
		}
	}

	return ErrClosed
}

func (b *Bot) createMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.api.Send(msg); err != nil {
		return err
	}
	return nil
}

func (b *Bot) createKeyboard(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = botKeyboard

	_, err := b.api.Send(msg)
	if err != nil {
		return err
	}
	return nil
}

func (b *Bot) updateKeyboard(callback *tgbotapi.CallbackQuery, text string) error {
	if text == callback.Message.Text {
		return nil
	}

	edit := tgbotapi.NewEditMessageText(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		text,
	)
	edit.ReplyMarkup = &botKeyboard

	if _, err := b.api.Send(edit); err != nil {
		return err
	}
	return nil
}

func (b *Bot) handleCommand(command *tgbotapi.Message) error {
	switch command.Text {
	case "/start":
		return b.createMessage(command.Chat.ID, b.welcome)
	case "/help":
		return b.createMessage(command.Chat.ID, b.help)
	case "/open":
		key := fmt.Sprintf("%d_%d",
			command.Chat.ID,
			command.From.ID,
		)

		if v := b.mc.Get(key); v != nil {
			return b.createMessage(
				command.Chat.ID,
				"Your session is not expired!",
			)
		}

		calculator := NewCalculator()
		err := b.createKeyboard(command.Chat.ID, calculator.Display)
		if err == nil {
			b.mc.Set(key, calculator)
		}
		return err
	default:
		return b.createMessage(command.Chat.ID, "Unknown command. Try /help")
	}
}

func (b *Bot) handleCallback(callback *tgbotapi.CallbackQuery) error {
	if _, err := b.api.Request(tgbotapi.NewCallback(callback.ID, "")); err != nil {
		return err
	}

	key := fmt.Sprintf("%d_%d",
		callback.Message.Chat.ID,
		callback.From.ID,
	)

	var calculator *Calculator
	if v := b.mc.Get(key); v == nil {
		err := b.updateKeyboard(
			callback,
			"Your session has expired, please /open a new one.",
		)

		if err != nil {
			return err
		}
		return ErrSessionExpired
	} else {
		calculator, _ = v.(*Calculator)
	}

	var err error
	if callback.Data == "AC" {
		calculator = NewCalculator()
	} else if len(callback.Data) != 1 {
		err = ErrUnsupported
	} else {
		r := rune(callback.Data[0])

		if strings.ContainsRune("0123456789.", r) {
			err = calculator.ProcessOperand(r)
		} else {
			err = calculator.ProcessOperator(r)
		}
	}

	if err != nil {
		return ErrUnsupported
	}

	err = b.updateKeyboard(callback, calculator.Display)
	if err == nil {
		b.mc.Set(key, calculator)
	}
	return err
}

func (b *Bot) Shutdown(ctx context.Context) error {
	b.inShutdown.Store(true)
	err := b.mc.Shutdown(ctx)
	b.api.StopReceivingUpdates()

	select {
	case <-b.isDone:
		if errors.Is(err, ErrMemcachedClosed) {
			return ErrClosed
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Bot) Close() error {
	b.inShutdown.Store(true)
	err := b.mc.Close()
	b.api.StopReceivingUpdates()
	<-b.isDone

	if errors.Is(err, ErrMemcachedClosed) {
		return ErrClosed
	}
	return err
}
