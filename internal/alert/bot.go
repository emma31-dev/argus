package alert

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// StatusProvider returns a plain-text status summary for the /status command.
type StatusProvider interface {
	StatusText(ctx context.Context) (string, error)
}

// StatusFunc adapts a function to StatusProvider.
type StatusFunc func(ctx context.Context) (string, error)

// StatusText implements StatusProvider.
func (f StatusFunc) StatusText(ctx context.Context) (string, error) { return f(ctx) }

// Bot is the long-polling Telegram command handler.
type Bot struct {
	tg          *Telegram
	subs        SubscriptionStore
	tokens      TokenLister
	status      StatusProvider // optional
	log         *slog.Logger
	PollSeconds int // getUpdates long-poll timeout, default 30
}

// NewBot builds a Bot. status may be nil.
func NewBot(tg *Telegram, subs SubscriptionStore, tokens TokenLister, status StatusProvider) *Bot {
	return &Bot{tg: tg, subs: subs, tokens: tokens, status: status, log: tg.cfg.Logger, PollSeconds: 30}
}

// ParseCommand splits a message such as "/subscribe@ArgusBot wNEAR" into the
// lower-case command ("subscribe") and its arguments. ok is false when the
// text is not a command.
func ParseCommand(text string) (cmd string, args []string, ok bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", nil, false
	}
	cmd = strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	if cmd == "" {
		return "", nil, false
	}
	return cmd, fields[1:], true
}

// Run long-polls Telegram until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	var offset int64
	failures := 0
	for ctx.Err() == nil {
		updates, err := b.tg.GetUpdates(ctx, offset, b.PollSeconds)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			failures++
			wait := min(time.Second<<min(failures, 6), time.Minute)
			var apiErr *APIError
			if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
				wait = time.Duration(apiErr.RetryAfter) * time.Second
			}
			b.log.Warn("telegram bot: getUpdates failed", "err", err, "retry_in", wait)
			_ = sleepCtx(ctx, wait)
			continue
		}
		failures = 0
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.Message == nil || u.Message.Text == "" {
				continue
			}
			chat := chatKey(u.Message.Chat.ID)
			reply, ok := b.HandleMessage(ctx, chat, u.Message.Text)
			if !ok {
				continue
			}
			if err := b.tg.SendMessage(ctx, chat, reply); err != nil {
				b.log.Warn("telegram bot: reply failed", "chat", chat, "err", err)
			}
		}
	}
	return nil
}

const botHelp = "Argus watches whether NEAR Intents holds the tokens it owes.\n\n" +
	"/subscribe &lt;token_id|symbol|all&gt; - get alerts when a token goes red\n" +
	"/unsubscribe &lt;token_id|symbol|all&gt; - stop alerts\n" +
	"/list - your subscriptions\n" +
	"/status - current monitor status"

// HandleMessage executes a command for a chat and returns the HTML reply.
// ok is false when the message is not a command (no reply is sent).
func (b *Bot) HandleMessage(ctx context.Context, chat, text string) (reply string, ok bool) {
	cmd, args, isCmd := ParseCommand(text)
	if !isCmd {
		return "", false
	}
	switch cmd {
	case "start", "help":
		return botHelp, true
	case "subscribe":
		return b.subscribe(ctx, chat, args), true
	case "unsubscribe":
		return b.unsubscribe(ctx, chat, args), true
	case "list":
		return b.list(ctx, chat), true
	case "status":
		if b.status == nil {
			return "Status is not available.", true
		}
		s, err := b.status.StatusText(ctx)
		if err != nil {
			b.log.Warn("telegram bot: status failed", "err", err)
			return "Could not fetch status right now.", true
		}
		return EscapeHTML(s), true
	default:
		return "Unknown command. Try /help.", true
	}
}

// resolve maps "all", a full token id, or a symbol (case-insensitive) to the
// matching token ids. A symbol shared by several tokens matches all of them.
func (b *Bot) resolve(ctx context.Context, arg string) (ids []string, label string, err error) {
	if strings.EqualFold(arg, "all") || arg == AllTokens {
		return []string{AllTokens}, "all tokens", nil
	}
	if b.tokens == nil {
		return nil, "", errors.New("token list unavailable")
	}
	list, err := b.tokens.ListTokens(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, t := range list {
		if t.TokenID == arg {
			return []string{t.TokenID}, t.Symbol, nil
		}
	}
	for _, t := range list {
		if strings.EqualFold(t.Symbol, arg) {
			ids = append(ids, t.TokenID)
		}
	}
	sort.Strings(ids)
	return ids, arg, nil
}

func (b *Bot) subscribe(ctx context.Context, chat string, args []string) string {
	if len(args) != 1 {
		return "Usage: /subscribe &lt;token_id|symbol|all&gt;"
	}
	ids, label, err := b.resolve(ctx, args[0])
	if err != nil {
		b.log.Warn("telegram bot: resolve failed", "err", err)
		return "Could not look up tokens right now, try again later."
	}
	if len(ids) == 0 {
		return fmt.Sprintf("No token matches <code>%s</code>.", EscapeHTML(args[0]))
	}
	for _, id := range ids {
		if _, err := b.subs.AddSubscription(ctx, Subscription{Channel: ChannelTelegram, Target: chat, TokenID: id}); err != nil {
			b.log.Error("telegram bot: subscribe failed", "err", err)
			return "Could not save the subscription, try again later."
		}
	}
	if len(ids) > 1 {
		return fmt.Sprintf("Subscribed to %d tokens matching <b>%s</b>.", len(ids), EscapeHTML(label))
	}
	return fmt.Sprintf("Subscribed to <b>%s</b>.", EscapeHTML(label))
}

func (b *Bot) unsubscribe(ctx context.Context, chat string, args []string) string {
	if len(args) != 1 {
		return "Usage: /unsubscribe &lt;token_id|symbol|all&gt;"
	}
	ids, _, err := b.resolve(ctx, args[0])
	if err != nil {
		return "Could not look up tokens right now, try again later."
	}
	removed := 0
	for _, id := range ids {
		ok, err := b.subs.RemoveSubscription(ctx, ChannelTelegram, chat, id)
		if err != nil {
			b.log.Error("telegram bot: unsubscribe failed", "err", err)
			return "Could not update the subscription, try again later."
		}
		if ok {
			removed++
		}
	}
	if removed == 0 {
		return "You were not subscribed to that."
	}
	return "Unsubscribed."
}

func (b *Bot) list(ctx context.Context, chat string) string {
	subs, err := b.subs.ListSubscriptions(ctx, ChannelTelegram, chat)
	if err != nil {
		return "Could not load subscriptions, try again later."
	}
	if len(subs) == 0 {
		return "No subscriptions. Use /subscribe &lt;token_id|symbol|all&gt;."
	}
	symbols := map[string]string{}
	if b.tokens != nil {
		if list, err := b.tokens.ListTokens(ctx); err == nil {
			for _, t := range list {
				symbols[t.TokenID] = t.Symbol
			}
		}
	}
	var sb strings.Builder
	sb.WriteString("Your subscriptions:\n")
	for _, s := range subs {
		if s.TokenID == AllTokens {
			sb.WriteString("- all tokens\n")
			continue
		}
		fmt.Fprintf(&sb, "- %s (<code>%s</code>)\n", EscapeHTML(symbols[s.TokenID]), EscapeHTML(s.TokenID))
	}
	return strings.TrimRight(sb.String(), "\n")
}
