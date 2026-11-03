package alert

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Notifier delivers one alert to its recipients. Notify must be safe to call
// again after an error (the Dispatcher retries) and should return nil when
// there is nothing to do (no recipients, throttled).
type Notifier interface {
	Name() string
	Notify(ctx context.Context, a Alert) error
}

// DispatcherConfig tunes retries.
type DispatcherConfig struct {
	MaxAttempts int           // per alert, default 5
	BaseBackoff time.Duration // default 1s; doubles each attempt
	MaxBackoff  time.Duration // default 1m
	PollWait    time.Duration // queue poll timeout, default 2s
	Logger      *slog.Logger
}

// Dispatcher reads alerts from a Queue and sends them through Notifiers with
// retries and exponential backoff. After MaxAttempts it dead-letters the
// alert (and logs it). When several notifiers are configured, only the ones
// that failed are retried.
type Dispatcher struct {
	cfg       DispatcherConfig
	queue     Queue
	notifiers []Notifier
	sleep     func(ctx context.Context, d time.Duration) error
}

// NewDispatcher builds a Dispatcher.
func NewDispatcher(cfg DispatcherConfig, q Queue, notifiers ...Notifier) *Dispatcher {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = time.Minute
	}
	if cfg.PollWait <= 0 {
		cfg.PollWait = 2 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Dispatcher{cfg: cfg, queue: q, notifiers: notifiers, sleep: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Run recovers in-flight alerts and then processes the queue until ctx is
// cancelled. It returns nil on cancellation.
func (d *Dispatcher) Run(ctx context.Context) error {
	if n, err := d.queue.Recover(ctx); err != nil {
		d.cfg.Logger.Error("alert dispatcher: recover failed", "err", err)
	} else if n > 0 {
		d.cfg.Logger.Info("alert dispatcher: re-queued in-flight alerts", "count", n)
	}
	failures := 0
	for ctx.Err() == nil {
		_, err := d.ProcessOne(ctx, d.cfg.PollWait)
		if err != nil && ctx.Err() == nil {
			failures++
			d.cfg.Logger.Error("alert dispatcher: queue error", "err", err)
			_ = d.sleep(ctx, min(d.cfg.BaseBackoff<<min(failures, 6), d.cfg.MaxBackoff))
			continue
		}
		failures = 0
	}
	return nil
}

// ProcessOne takes at most one alert off the queue (waiting up to wait) and
// delivers it. It reports whether an alert was handled.
func (d *Dispatcher) ProcessOne(ctx context.Context, wait time.Duration) (bool, error) {
	del, err := d.queue.Dequeue(ctx, wait)
	if err != nil || del == nil {
		return false, err
	}
	a := del.Alert
	log := d.cfg.Logger.With("kind", string(a.Kind), "token", a.TokenID, "incident", a.IncidentID)
	pending := d.notifiers
	var errs []error
	for attempt := 1; attempt <= d.cfg.MaxAttempts; attempt++ {
		var failed []Notifier
		errs = errs[:0]
		for _, n := range pending {
			if err := n.Notify(ctx, a); err != nil {
				failed = append(failed, n)
				errs = append(errs, fmt.Errorf("%s: %w", n.Name(), err))
			}
		}
		pending = failed
		if len(pending) == 0 {
			log.Info("alert delivered", "attempt", attempt)
			return true, d.queue.Ack(ctx, del)
		}
		if ctx.Err() != nil {
			// Leave the delivery in flight; Recover re-queues it on restart.
			return true, ctx.Err()
		}
		log.Warn("alert delivery failed", "attempt", attempt, "err", errors.Join(errs...))
		if attempt < d.cfg.MaxAttempts {
			backoff := min(d.cfg.BaseBackoff<<(attempt-1), d.cfg.MaxBackoff)
			if err := d.sleep(ctx, backoff); err != nil {
				return true, err
			}
		}
	}
	reason := fmt.Sprintf("gave up after %d attempts: %v", d.cfg.MaxAttempts, errors.Join(errs...))
	log.Error("alert dead-lettered", "reason", reason)
	return true, d.queue.Dead(ctx, del, reason)
}
