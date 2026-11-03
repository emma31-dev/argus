package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// SignatureHeader carries "sha256=<hex hmac of the body>".
const SignatureHeader = "X-Argus-Signature"

// SignPayload returns the X-Argus-Signature value for body under secret.
func SignPayload(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// WebhookConfig configures the webhook notifier.
type WebhookConfig struct {
	// Timeout per request, default 5s.
	Timeout time.Duration
	// AllowPrivate permits webhook targets on loopback/private addresses.
	// Leave false in production to avoid SSRF; tests set it.
	AllowPrivate   bool
	HTTPClient     *http.Client // overrides Timeout/AllowPrivate when set
	UpdateInterval time.Duration
	Logger         *slog.Logger
	Now            func() time.Time
}

// Webhook is a Notifier that POSTs alerts as JSON to subscribed URLs, signed
// with a per-subscription HMAC-SHA256 secret.
type Webhook struct {
	cfg      WebhookConfig
	subs     SubscriptionStore
	http     *http.Client
	throttle *updateThrottle
	sent     *sentSet
}

// ValidateWebhookURL checks that raw is an absolute http(s) URL.
func ValidateWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("webhook url must be an absolute http(s) url")
	}
	return nil
}

// NewWebhook builds a webhook notifier.
func NewWebhook(cfg WebhookConfig, subs SubscriptionStore) *Webhook {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
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
		d := &net.Dialer{Timeout: cfg.Timeout}
		if !cfg.AllowPrivate {
			d.Control = denyPrivate
		}
		hc = &http.Client{Timeout: cfg.Timeout, Transport: &http.Transport{DialContext: d.DialContext}}
	}
	return &Webhook{
		cfg: cfg, subs: subs, http: hc,
		throttle: newUpdateThrottle(cfg.UpdateInterval, cfg.Now),
		sent:     newSentSet(time.Hour, cfg.Now),
	}
}

// denyPrivate is a net.Dialer.Control that refuses non-public destinations.
func denyPrivate(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("webhook target %s is not a public address", host)
	}
	return nil
}

// Name implements Notifier.
func (w *Webhook) Name() string { return ChannelWebhook }

// Notify implements Notifier.
func (w *Webhook) Notify(ctx context.Context, a Alert) error {
	if a.Kind == IncidentUpdate && !w.throttle.ok(a.IncidentID) {
		return nil
	}
	subs, err := w.subs.SubscriptionsForToken(ctx, ChannelWebhook, a.TokenID)
	if err != nil {
		return fmt.Errorf("list subscriptions: %w", err)
	}
	if len(subs) == 0 {
		return nil
	}
	body, err := json.Marshal(a)
	if err != nil {
		return err
	}
	var errs []error
	seen := map[string]bool{}
	for _, s := range subs {
		if seen[s.Target] {
			continue
		}
		seen[s.Target] = true
		key := a.key() + "|" + s.Target
		if w.sent.has(key) {
			continue
		}
		if err := w.post(ctx, s, a, body); err != nil {
			errs = append(errs, err)
			continue
		}
		w.sent.add(key)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	switch a.Kind {
	case IncidentUpdate:
		w.throttle.mark(a.IncidentID)
	case Recovered:
		w.throttle.forget(a.IncidentID)
	}
	return nil
}

func (w *Webhook) post(ctx context.Context, s Subscription, a Alert, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, w.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook %s: %w", redactURL(s.Target), err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "argus-alerts/1")
	req.Header.Set("X-Argus-Event", string(a.Kind))
	if s.Secret != "" {
		req.Header.Set(SignatureHeader, SignPayload(s.Secret, body))
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return fmt.Errorf("webhook %s: %w", redactURL(s.Target), unwrapURLErr(err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook %s: status %d", redactURL(s.Target), resp.StatusCode)
	}
	return nil
}

// redactURL keeps scheme and host only (paths and queries may hold secrets).
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}
	return u.Scheme + "://" + u.Host
}

// unwrapURLErr drops the URL (which may embed secrets) from a client error.
func unwrapURLErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
