package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// TelegramDefaultBaseURL is the public Bot API endpoint.
const TelegramDefaultBaseURL = "https://api.telegram.org"

// TelegramConfig configures the Telegram client/notifier.
type TelegramConfig struct {
	Token         string // bot token; never logged
	BaseURL       string // default TelegramDefaultBaseURL (override in tests)
	DefaultChatID string // optional chat that receives every alert
	HTTPClient    *http.Client
	// UpdateInterval is the minimum gap between IncidentUpdate messages per
	// incident (default 10 minutes).
	UpdateInterval time.Duration
	Logger         *slog.Logger
	Now            func() time.Time
}

// APIError is an error response from the Telegram Bot API.
type APIError struct {
	Code        int
	Description string
	// RetryAfterSeconds is parameters.retry_after of a 429 response (0 if
	// absent).
	RetryAfterSeconds int
}

// RetryAfter implements the retry hint the Dispatcher honours: how long
// Telegram asked us to wait before the next request.
func (e *APIError) RetryAfter() time.Duration {
	return time.Duration(e.RetryAfterSeconds) * time.Second
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram: %d %s", e.Code, e.Description)
}

// permanent reports whether retrying cannot help for this recipient (the
// chat blocked the bot, was deleted, or the request itself is invalid).
func (e *APIError) permanent() bool {
	return e.Code >= 400 && e.Code < 500 && e.Code != 429 && e.Code != 408
}

// Telegram is a Notifier that sends alerts through the Telegram Bot API to
// the default chat and to per-user subscriptions. The same type provides the
// low-level calls used by Bot.
type Telegram struct {
	cfg      TelegramConfig
	subs     SubscriptionStore // may be nil
	http     *http.Client
	throttle *updateThrottle
	sent     *sentSet
}

// NewTelegram builds a Telegram client. subs may be nil when only the default
// chat is used.
func NewTelegram(cfg TelegramConfig, subs SubscriptionStore) (*Telegram, error) {
	if cfg.Token == "" {
		return nil, errors.New("telegram: empty bot token")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = TelegramDefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.UpdateInterval <= 0 {
		cfg.UpdateInterval = 10 * time.Minute
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}
	return &Telegram{
		cfg: cfg, subs: subs, http: hc,
		throttle: newUpdateThrottle(cfg.UpdateInterval, cfg.Now),
		sent:     newSentSet(time.Hour, cfg.Now),
	}, nil
}

// Name implements Notifier.
func (t *Telegram) Name() string { return ChannelTelegram }

// redact removes the bot token from text (http errors embed the URL).
func (t *Telegram) redact(s string) string {
	return strings.ReplaceAll(s, t.cfg.Token, "<redacted>")
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// call performs one Bot API method call. Errors never contain the token.
func (t *Telegram) call(ctx context.Context, method string, payload any, timeout time.Duration, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.BaseURL+"/bot"+t.cfg.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return errors.New(t.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %s", method, t.redact(err.Error()))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("telegram %s: read body: %s", method, t.redact(err.Error()))
	}
	var r apiResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return &APIError{Code: resp.StatusCode, Description: "non-JSON response"}
	}
	if !r.OK {
		code := r.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &APIError{Code: code, Description: r.Description, RetryAfterSeconds: r.Parameters.RetryAfter}
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

// SendMessage sends HTML-formatted text to a chat.
func (t *Telegram) SendMessage(ctx context.Context, chatID, htmlText string) error {
	return t.call(ctx, "sendMessage", map[string]any{
		"chat_id":                  chatID,
		"text":                     htmlText,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}, 15*time.Second, nil)
}

// Notify implements Notifier.
func (t *Telegram) Notify(ctx context.Context, a Alert) error {
	if a.Kind == IncidentUpdate && !t.throttle.ok(a.IncidentID) {
		t.cfg.Logger.Debug("telegram: incident update throttled", "incident", a.IncidentID)
		return nil
	}
	recipients := map[string]bool{}
	if t.cfg.DefaultChatID != "" {
		recipients[t.cfg.DefaultChatID] = true
	}
	if t.subs != nil {
		subs, err := t.subs.SubscriptionsForToken(ctx, ChannelTelegram, a.TokenID)
		if err != nil {
			return fmt.Errorf("list subscriptions: %w", err)
		}
		for _, s := range subs {
			recipients[s.Target] = true
		}
	}
	if len(recipients) == 0 {
		return nil
	}
	text := FormatTelegram(a)
	var errs []error
	for chat := range recipients {
		key := a.key() + "|" + chat
		if t.sent.has(key) {
			continue
		}
		err := t.SendMessage(ctx, chat, text)
		var apiErr *APIError
		switch {
		case err == nil:
			t.sent.add(key)
		case errors.As(err, &apiErr) && apiErr.permanent():
			t.cfg.Logger.Warn("telegram: recipient rejected message, dropping", "chat", chat, "err", err)
			t.sent.add(key)
			if apiErr.Code == http.StatusForbidden && t.subs != nil && chat != t.cfg.DefaultChatID {
				// The user blocked the bot; stop trying.
				if _, rerr := t.subs.RemoveSubscription(ctx, ChannelTelegram, chat, ""); rerr != nil {
					t.cfg.Logger.Warn("telegram: removing blocked subscription failed", "err", rerr)
				}
			}
		default:
			errs = append(errs, fmt.Errorf("chat %s: %w", chat, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	switch a.Kind {
	case IncidentUpdate:
		t.throttle.mark(a.IncidentID)
	case Recovered:
		t.throttle.forget(a.IncidentID)
	}
	return nil
}

// EscapeHTML escapes text for Telegram's HTML parse mode (only &, < and >
// need escaping; quotes too inside attribute values, see escapeAttr).
func EscapeHTML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func escapeAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// FormatTelegram renders an alert as Telegram HTML.
func FormatTelegram(a Alert) string {
	sym := EscapeHTML(a.Symbol)
	var b strings.Builder
	deficit := func(gap string) string {
		d := negate(gap)
		return FormatUnits(d, a.Decimals) + " " + a.Symbol
	}
	switch a.Kind {
	case TurnedRed:
		fmt.Fprintf(&b, "\U0001F534 <b>RED: %s</b>\n", sym)
		b.WriteString("NEAR Intents holds less of this token than it owes.\n")
		fmt.Fprintf(&b, "Deficit: <b>%s</b>\n", EscapeHTML(deficit(a.Gap)))
	case IncidentUpdate:
		fmt.Fprintf(&b, "\U0001F7E0 <b>UPDATE: %s deficit grew</b>\n", sym)
		fmt.Fprintf(&b, "Current deficit: <b>%s</b>\n", EscapeHTML(deficit(a.Gap)))
		fmt.Fprintf(&b, "Max deficit: %s\n", EscapeHTML(FormatUnits(a.MaxDeficit, a.Decimals)+" "+a.Symbol))
	case Recovered:
		fmt.Fprintf(&b, "\U0001F7E2 <b>RECOVERED: %s</b>\n", sym)
		b.WriteString("Held balance is back at or above the owed total.\n")
		fmt.Fprintf(&b, "Max deficit: %s\n", EscapeHTML(FormatUnits(a.MaxDeficit, a.Decimals)+" "+a.Symbol))
		if a.DurationSeconds > 0 {
			fmt.Fprintf(&b, "Duration: %s\n", (time.Duration(a.DurationSeconds) * time.Second).String())
		}
	default:
		fmt.Fprintf(&b, "<b>%s: %s</b>\n", EscapeHTML(string(a.Kind)), sym)
	}
	fmt.Fprintf(&b, "Token: <code>%s</code>\n", EscapeHTML(a.TokenID))
	if a.Kind == Recovered {
		fmt.Fprintf(&b, "Started at block %d, recovered at block %d\n", a.StartBlock, a.Block)
	} else {
		fmt.Fprintf(&b, "First negative block: %d, current block: %d\n", a.StartBlock, a.Block)
	}
	if a.IncidentID != 0 {
		fmt.Fprintf(&b, "Incident #%d\n", a.IncidentID)
	}
	if a.DashboardURL != "" {
		fmt.Fprintf(&b, "<a href=\"%s\">Open dashboard</a>\n", escapeAttr(a.DashboardURL))
	}
	return strings.TrimRight(b.String(), "\n")
}

// Update is the subset of a Bot API Update that the bot uses.
type Update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

// GetUpdates long-polls for updates after offset.
func (t *Telegram) GetUpdates(ctx context.Context, offset int64, pollSeconds int) ([]Update, error) {
	var out []Update
	err := t.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": pollSeconds, "allowed_updates": []string{"message"},
	}, time.Duration(pollSeconds+15)*time.Second, &out)
	return out, err
}

func chatKey(id int64) string { return strconv.FormatInt(id, 10) }
