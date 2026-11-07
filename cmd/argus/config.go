package main

import (
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/events"
)

// DefaultRPCURLs are the free public mainnet endpoints used when
// ARGUS_RPC_URLS is unset. The NEAR-hosted rpc.mainnet.near.org is left out
// on purpose: it is deprecated and capped at 30 requests a minute.
var DefaultRPCURLs = []string{"https://free.rpc.fastnear.com", "https://rpc.mainnet.fastnear.com", "https://near.drpc.org", "https://rpc.intea.rs"}

// DefaultRPCRPS is the per-provider request rate when ARGUS_RPC_RPS is unset.
// FastNEAR's free endpoint answered 429 for a third of the calls at 2 a
// second and accepted about 1 a second sustained, so that is the default.
// Raise it only with a keyed or self-hosted endpoint.
const DefaultRPCRPS = 1

// Config is the monitor's configuration, read from the environment (names
// are documented in .env.example).
type Config struct {
	HTTPAddr    string
	DatabaseURL string
	RedisURL    string

	RPCURLs []string
	RPCRPS  float64

	IntentsAccount string
	TokensFile     string
	OneClickURL    string

	Interval time.Duration
	K        int
	// RedThresholds are absolute deficits (smallest units) that turn a token
	// red immediately, without waiting for K snapshots. Key "*" is the
	// default for every token.
	RedThresholds     map[string]*big.Int
	BatchSize         int
	Concurrency       int
	DiscoveryInterval time.Duration

	TelegramBotToken string
	TelegramChatID   string
	PublicURL        string

	APIRateLimit int
	TrustProxy   bool
	CORSOrigins  []string
	ReplayFile   string

	LLMEnabled bool
	LLMBaseURL string
	LLMAPIKey  string
	LLMModel   string

	LogLevel  string
	Retention time.Duration

	EventsEnabled  bool
	NeardataURL    string
	NeardataRPS    float64
	NeardataAPIKey string
}

// LoadConfig parses the environment through getenv. Every problem is
// reported, not just the first. requireDB is false for -once, which keeps
// state in memory.
func LoadConfig(getenv func(string) string, requireDB bool) (Config, error) {
	var errs []error
	str := func(name, def string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}
		return def
	}
	dur := func(name, def string) time.Duration {
		raw := str(name, def)
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s=%q: want a positive duration such as 30s or 24h", name, raw))
			d, _ = time.ParseDuration(def)
		}
		return d
	}
	integer := func(name string, def, min int) int {
		raw := str(name, strconv.Itoa(def))
		n, err := strconv.Atoi(raw)
		if err != nil || n < min {
			errs = append(errs, fmt.Errorf("%s=%q: want an integer >= %d", name, raw, min))
			return def
		}
		return n
	}
	boolean := func(name string) bool {
		raw := strings.ToLower(str(name, "false"))
		switch raw {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
		errs = append(errs, fmt.Errorf("%s=%q: want true or false", name, raw))
		return false
	}

	c := Config{
		HTTPAddr:       str("ARGUS_HTTP_ADDR", ":8080"),
		DatabaseURL:    str("DATABASE_URL", ""),
		RedisURL:       str("REDIS_URL", ""),
		IntentsAccount: str("ARGUS_INTENTS_ACCOUNT", "intents.near"),
		TokensFile:     str("ARGUS_TOKENS_FILE", ""),
		OneClickURL:    str("ARGUS_ONECLICK_TOKENS_URL", ""),
		PublicURL:      str("ARGUS_PUBLIC_URL", ""),
		ReplayFile:     str("ARGUS_REPLAY_FILE", "data/replay/2026-10-01.json"),
		LogLevel:       strings.ToLower(str("ARGUS_LOG_LEVEL", "info")),
		LLMBaseURL:     str("LLM_BASE_URL", ""),
		LLMAPIKey:      str("LLM_API_KEY", ""),
		LLMModel:       str("LLM_MODEL", ""),

		TelegramBotToken: str("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:   str("TELEGRAM_CHAT_ID", ""),
	}
	errsBefore := len(errs)
	for _, u := range strings.Split(getenv("ARGUS_RPC_URLS"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			if p, err := url.Parse(u); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
				errs = append(errs, fmt.Errorf("ARGUS_RPC_URLS: %q is not an http(s) URL", redactForError(u)))
				continue
			}
			c.RPCURLs = append(c.RPCURLs, u)
		}
	}
	if len(c.RPCURLs) == 0 && len(errs) == errsBefore {
		c.RPCURLs = append(c.RPCURLs, DefaultRPCURLs...)
	}
	rps := str("ARGUS_RPC_RPS", strconv.Itoa(DefaultRPCRPS))
	if v, err := strconv.ParseFloat(rps, 64); err != nil || v <= 0 {
		errs = append(errs, fmt.Errorf("ARGUS_RPC_RPS=%q: want a positive number", rps))
		c.RPCRPS = DefaultRPCRPS
	} else {
		c.RPCRPS = v
	}
	c.Interval = dur("ARGUS_INTERVAL", "30s")
	c.DiscoveryInterval = dur("ARGUS_DISCOVERY_INTERVAL", "24h")
	c.Retention = dur("ARGUS_RETENTION", "720h")
	c.K = integer("ARGUS_DEBOUNCE_K", 4, 1)
	if th, err := parseThresholds(getenv("ARGUS_RED_THRESHOLDS")); err != nil {
		errs = append(errs, err)
	} else {
		c.RedThresholds = th
	}
	c.BatchSize = integer("ARGUS_BATCH_SIZE", 50, 1)
	c.Concurrency = integer("ARGUS_CONCURRENCY", 8, 1)
	c.APIRateLimit = integer("ARGUS_API_RATE_LIMIT", 60, 0)
	c.TrustProxy = boolean("ARGUS_TRUST_PROXY")
	c.LLMEnabled = boolean("LLM_ENABLED")
	c.EventsEnabled = boolean("ARGUS_EVENTS_ENABLED")
	c.NeardataURL = str("ARGUS_NEARDATA_URL", events.DefaultBaseURL)
	c.NeardataAPIKey = str("ARGUS_NEARDATA_API_KEY", "")
	nrps := str("ARGUS_NEARDATA_RPS", "2")
	if v, err := strconv.ParseFloat(nrps, 64); err != nil || v <= 0 {
		errs = append(errs, fmt.Errorf("ARGUS_NEARDATA_RPS=%q: want a positive number", nrps))
		c.NeardataRPS = events.DefaultRPS
	} else {
		c.NeardataRPS = v
	}
	for _, o := range strings.Split(str("ARGUS_CORS_ORIGINS", "*"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.CORSOrigins = append(c.CORSOrigins, o)
		}
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("ARGUS_LOG_LEVEL=%q: want debug, info, warn or error", c.LogLevel))
		c.LogLevel = "info"
	}
	if requireDB && c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.LLMEnabled && (c.LLMBaseURL == "" || c.LLMModel == "") {
		errs = append(errs, errors.New("LLM_ENABLED=true needs LLM_BASE_URL and LLM_MODEL"))
	}
	if c.TelegramBotToken == "" && c.TelegramChatID != "" {
		errs = append(errs, errors.New("TELEGRAM_CHAT_ID is set but TELEGRAM_BOT_TOKEN is not"))
	}
	if c.PublicURL != "" {
		if p, err := url.Parse(c.PublicURL); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			errs = append(errs, fmt.Errorf("ARGUS_PUBLIC_URL=%q: want an absolute http(s) URL", c.PublicURL))
		}
	}
	return c, errors.Join(errs...)
}

// redactForError keeps scheme and host of a URL that may embed an API key.
func redactForError(raw string) string {
	if p, err := url.Parse(raw); err == nil && p.Host != "" {
		return p.Scheme + "://" + p.Host + "/..."
	}
	if len(raw) > 12 {
		return raw[:12] + "..."
	}
	return raw
}

// providerNames names an RPC provider by host, adding #n for duplicates.
func providerNames(urls []string) []string {
	seen := map[string]int{}
	out := make([]string, len(urls))
	for i, u := range urls {
		host := u
		if p, err := url.Parse(u); err == nil && p.Host != "" {
			host = p.Host
		}
		seen[host]++
		out[i] = host
		if n := seen[host]; n > 1 {
			out[i] = fmt.Sprintf("%s#%d", host, n)
		}
	}
	return out
}

// parseThresholds parses ARGUS_RED_THRESHOLDS: comma-separated
// "<token id>=<deficit in smallest units>" pairs, with "*" as the default
// for all tokens. Token ids contain ':' but never '=' or ','.
func parseThresholds(raw string) (map[string]*big.Int, error) {
	out := map[string]*big.Int{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, amount, ok := strings.Cut(part, "=")
		id, amount = strings.TrimSpace(id), strings.TrimSpace(amount)
		v, okNum := new(big.Int).SetString(amount, 10)
		if !ok || id == "" || !okNum || v.Sign() <= 0 {
			return nil, fmt.Errorf("ARGUS_RED_THRESHOLDS: %q: want <token id or *>=<positive integer in smallest units>", part)
		}
		out[id] = v
	}
	return out, nil
}

// alertConfig builds the debounce configuration from c.
func (c Config) alertConfig() alert.Config {
	ac := alert.Config{K: c.K}
	for id, v := range c.RedThresholds {
		if id == "*" {
			ac.DefaultAbsThreshold = v
			continue
		}
		if ac.AbsThreshold == nil {
			ac.AbsThreshold = map[string]*big.Int{}
		}
		ac.AbsThreshold[id] = v
	}
	return ac
}
