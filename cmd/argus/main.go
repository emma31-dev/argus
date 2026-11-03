// Command argus is the Argus monitor: it discovers the tokens NEAR Intents
// supports, snapshots owed vs held for each of them at the same block every
// interval, drives the alert state machine, and serves the read-only API.
//
// Usage:
//
//	argus               run the monitor (configuration from the environment)
//	argus -once         one discovery + one snapshot round, print a table, exit
//	argus -healthcheck  GET /healthz on the local server (used by Docker)
//	argus -version      print the build version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/store"
	"github.com/emma31-dev/argus/internal/summary"
)

// version is set at build time: -ldflags "-X main.version=...".
var version = "dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print the version and exit")
		health      = flag.Bool("healthcheck", false, "GET /healthz on this machine's ARGUS_HTTP_ADDR; exit 0 if healthy, 1 if not")
		once        = flag.Bool("once", false, "run one discovery and one snapshot round, print a status table and exit")
	)
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Println("argus", version)
	case *health:
		if err := healthcheck(os.Getenv("ARGUS_HTTP_ADDR"), 5*time.Second); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy:", err)
			os.Exit(1)
		}
	case *once:
		cfg, err := LoadConfig(os.Getenv, false)
		if err != nil {
			fmt.Fprintln(os.Stderr, "configuration error:\n"+err.Error())
			os.Exit(2)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runOnce(ctx, cfg, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		cfg, err := LoadConfig(os.Getenv, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "configuration error:\n"+err.Error())
			os.Exit(2)
		}
		log := newLogger(cfg.LogLevel, os.Stderr)
		slog.SetDefault(log)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runMonitor(ctx, cfg, log); err != nil {
			log.Error("argus stopped with an error", "err", err)
			os.Exit(1)
		}
		log.Info("argus stopped")
	}
}

func newLogger(level string, w io.Writer) *slog.Logger {
	var lv slog.Level
	_ = lv.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lv}))
}

// healthcheck probes the local server. addr is ARGUS_HTTP_ADDR (":8080",
// "0.0.0.0:8080", "127.0.0.1:9000"); the probe always targets 127.0.0.1.
func healthcheck(addr string, timeout time.Duration) error {
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bad ARGUS_HTTP_ADDR %q: %w", addr, err)
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/healthz returned %s", resp.Status)
	}
	return nil
}

// newRPC builds the RPC client from the configured providers.
func newRPC(cfg Config, opts ...rpc.Option) *rpc.Client {
	names := providerNames(cfg.RPCURLs)
	ps := make([]rpc.Provider, len(cfg.RPCURLs))
	for i, u := range cfg.RPCURLs {
		ps[i] = rpc.Provider{Name: names[i], URL: u, RPS: cfg.RPCRPS, Burst: max(1, int(cfg.RPCRPS))}
	}
	opts = append([]rpc.Option{rpc.WithHTTPClient(&http.Client{Timeout: 30 * time.Second})}, opts...)
	return rpc.New(ps, opts...)
}

// openStore connects to Postgres, retrying briefly so that a slow-starting
// database does not crash-loop the process, and applies migrations.
func openStore(ctx context.Context, url string, log *slog.Logger) (*store.Repo, func(), error) {
	var lastErr error
	for attempt := 1; attempt <= 10; attempt++ {
		pool, err := store.Open(ctx, url)
		if err == nil {
			if err := store.Migrate(ctx, pool); err != nil {
				pool.Close()
				return nil, nil, fmt.Errorf("migrate: %w", err)
			}
			return store.New(pool), pool.Close, nil
		}
		lastErr = err
		log.Warn("postgres not ready", "attempt", attempt, "err", err)
		if !sleep(ctx, time.Duration(attempt)*time.Second) {
			return nil, nil, ctx.Err()
		}
	}
	return nil, nil, fmt.Errorf("postgres unavailable: %w", lastErr)
}

// openQueue returns the Redis queue when REDIS_URL is set, else the
// in-memory queue (single process; queued alerts are lost on restart).
func openQueue(ctx context.Context, cfg Config, log *slog.Logger) (alert.Queue, func(), error) {
	if cfg.RedisURL == "" {
		log.Warn("REDIS_URL is not set: using the in-memory alert queue; undelivered alerts are lost on restart")
		return alert.NewMemoryQueue(), func() {}, nil
	}
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, nil, errors.New("REDIS_URL is not a valid redis:// URL")
	}
	rdb := redis.NewClient(opts)
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := rdb.Ping(pctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, nil, fmt.Errorf("redis ping: %w", err)
	}
	return alert.NewRedisQueue(rdb, ""), func() { _ = rdb.Close() }, nil
}

// runMonitor runs everything until ctx is cancelled.
func runMonitor(ctx context.Context, cfg Config, log *slog.Logger) error {
	log.Info("argus starting", "version", version, "interval", cfg.Interval.String(), "debounce_k", cfg.K,
		"providers", providerNames(cfg.RPCURLs), "http_addr", cfg.HTTPAddr)

	repo, closeDB, err := openStore(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer closeDB()
	queue, closeQueue, err := openQueue(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer closeQueue()

	app := NewApp(cfg, log, repo, newRPC(cfg), queue)

	if cfg.EventsEnabled {
		app.EnableEvents(ctx, newNeardata(cfg))
		log.Info("event stream enabled", "neardata", cfg.NeardataURL, "rps", cfg.NeardataRPS)
	}

	notifiers, bot, err := buildNotifiers(ctx, cfg, log, repo, app)
	if err != nil {
		return err
	}
	disp := alert.NewDispatcher(alert.DispatcherConfig{Logger: log}, queue, notifiers...)

	srv := app.APIServer(version)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	fail := func(err error) {
		errOnce.Do(func() { firstErr = err })
		cancel()
	}
	run := func(name string, f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Error("goroutine panicked", "name", name, "panic", r)
					fail(fmt.Errorf("%s panicked: %v", name, r))
				}
			}()
			f()
		}()
	}

	run("http", func() {
		log.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fail(fmt.Errorf("http server: %w", err))
		}
	})
	run("http-shutdown", func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		if err := srv.Shutdown(sctx); err != nil {
			log.Warn("http shutdown", "err", err)
		}
	})
	run("discovery", func() { app.discoveryLoop(ctx) })
	run("snapshots", func() { app.snapshotLoop(ctx) })
	run("alerts", func() { _ = disp.Run(ctx) })
	run("prune", func() { app.prune(ctx); app.pruneLoop(ctx) })
	if app.follower != nil {
		run("events-follower", func() {
			if err := app.follower.Run(ctx); err != nil && ctx.Err() == nil {
				log.Error("event follower stopped", "err", err)
			}
		})
	}
	if bot != nil {
		run("telegram-bot", func() {
			if err := bot.Run(ctx); err != nil && ctx.Err() == nil {
				log.Error("telegram bot stopped", "err", err)
			}
		})
	}

	<-ctx.Done()
	log.Info("shutting down")
	wg.Wait()
	app.WaitEvents()
	return firstErr
}

// buildNotifiers creates the alert channels: Telegram (when configured),
// webhooks (always; they only send to subscribers) and, last, the optional
// LLM summariser (which never fails an alert).
func buildNotifiers(ctx context.Context, cfg Config, log *slog.Logger, repo *store.Repo, app *App) ([]alert.Notifier, *alert.Bot, error) {
	var notifiers []alert.Notifier
	var bot *alert.Bot
	if cfg.TelegramBotToken != "" {
		tg, err := alert.NewTelegram(alert.TelegramConfig{
			Token: cfg.TelegramBotToken, DefaultChatID: cfg.TelegramChatID, Logger: log,
		}, repo)
		if err != nil {
			return nil, nil, err
		}
		notifiers = append(notifiers, tg)
		bot = alert.NewBot(tg, repo, repo, alert.StatusFunc(app.StatusText))
		log.Info("telegram alerts enabled", "default_chat", cfg.TelegramChatID != "")
	} else {
		log.Info("telegram alerts disabled (TELEGRAM_BOT_TOKEN not set)")
	}
	notifiers = append(notifiers, alert.NewWebhook(alert.WebhookConfig{Logger: log}, repo))
	if cfg.LLMEnabled {
		s, err := summary.New(ctx, summary.Config{
			BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel, Logger: log,
		}, repo)
		if err != nil {
			return nil, nil, err
		}
		notifiers = append(notifiers, s)
		log.Info("llm incident summaries enabled", "model", cfg.LLMModel)
	}
	return notifiers, bot, nil
}
