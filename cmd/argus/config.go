package main

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/emma31-dev/argus/internal/events"
)

// DefaultRPCURLs are the free public mainnet endpoints used when
// ARGUS_RPC_URLS is unset (see docs/research.md).
var DefaultRPCURLs = []string{"https://free.rpc.fastnear.com", "https://rpc.mainnet.fastnear.com", "https://rpc.mainnet.near.org"}

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

	Interval          time.Duration
	K                 int
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
		ReplayFile:     str("ARGUS_REPLAY_FILE", "evidence/replay-2026-10-01.json"),
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
	rps := str("ARGUS_RPC_RPS", "5")
	if v, err := strconv.ParseFloat(rps, 64); err != nil || v <= 0 {
		errs = append(errs, fmt.Errorf("ARGUS_RPC_RPS=%q: want a positive number", rps))
		c.RPCRPS = 5
	} else {
		c.RPCRPS = v
	}
	c.Interval = dur("ARGUS_INTERVAL", "30s")
	c.DiscoveryInterval = dur("ARGUS_DISCOVERY_INTERVAL", "24h")
	c.Retention = dur("ARGUS_RETENTION", "720h")
	c.K = integer("ARGUS_DEBOUNCE_K", 4, 1)
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
