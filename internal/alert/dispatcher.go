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

// RetryAfterer is implemented by notifier errors that carry the server's
// requested wait (Telegram's 429 retry_after). The Dispatcher waits at least
// that long before the next attempt.
type RetryAfterer interface {
	RetryAfter() time.Duration
}

// maxRetryAfter bounds a server-requested wait so a bogus hint cannot park
// the dispatcher for hours.
const maxRetryAfter = time.Hour

// DispatcherConfig tunes retries.
//
// The defaults (12 attempts, 2 s doubling to a 5 min cap) retry one alert for
// about 23 minutes in total (waits of 2, 4, 8, ... 256 s and then 5 min), long
// enough to ride out a Telegram or network outage; alerts that still fail
// are dead-lettered and re-queued by the periodic dead-letter requeue.
type DispatcherConfig struct {
	MaxAttempts int           // per alert, default 12
	BaseBackoff time.Duration // default 2s; doubles each attempt
	MaxBackoff  time.Duration // default 5m
	PollWait    time.Duration // queue poll timeout, default 2s
	// RequeueEvery is how often dead-lettered alerts younger than
	// DeadRequeueMaxAge are put back on the queue (default 15m; negative
	// disables). RequeueMax bounds how many move per pass (default 50).
	RequeueEvery time.Duration
	RequeueMax   int
	Logger       *slog.Logger
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
	now       func() time.Time
}

// NewDispatcher builds a Dispatcher.
func NewDispatcher(cfg DispatcherConfig, q Queue, notifiers ...Notifier) *Dispatcher {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 12
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 2 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 5 * time.Minute
	}
	if cfg.RequeueEvery == 0 {
		cfg.RequeueEvery = 15 * time.Minute
	}
	if cfg.RequeueMax <= 0 {
		cfg.RequeueMax = 50
	}
	if cfg.PollWait <= 0 {
		cfg.PollWait = 2 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Dispatcher{cfg: cfg, queue: q, notifiers: notifiers, sleep: sleepCtx, now: time.Now}
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
	lastRequeue := d.now()
	for ctx.Err() == nil {
		if d.cfg.RequeueEvery > 0 && d.now().Sub(lastRequeue) >= d.cfg.RequeueEvery {
			lastRequeue = d.now()
			d.requeueDead(ctx)
		}
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

// requeueDead puts recently dead-lettered alerts back on the queue.
func (d *Dispatcher) requeueDead(ctx context.Context) {
	n, err := d.queue.RequeueDead(ctx, d.cfg.RequeueMax)
	if err != nil {
		if ctx.Err() == nil {
			d.cfg.Logger.Error("alert dispatcher: dead-letter requeue failed", "err", err)
		}
		return
	}
	if n > 0 {
		d.cfg.Logger.Info("alert dispatcher: re-queued dead-lettered alerts", "count", n)
	}
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
			backoff := min(d.cfg.BaseBackoff<<min(attempt-1, 30), d.cfg.MaxBackoff)
			if ra := retryAfter(errs); ra > backoff {
				backoff = ra
			}
			if err := d.sleep(ctx, backoff); err != nil {
				return true, err
			}
		}
	}
	reason := fmt.Sprintf("gave up after %d attempts: %v", d.cfg.MaxAttempts, errors.Join(errs...))
	log.Error("alert dead-lettered", "reason", reason)
	return true, d.queue.Dead(ctx, del, reason)
}

// retryAfter is the longest server-requested wait among errs, bounded by
// maxRetryAfter.
func retryAfter(errs []error) time.Duration {
	var longest time.Duration
	for _, err := range errs {
		var ra RetryAfterer
		if errors.As(err, &ra) {
			longest = max(longest, ra.RetryAfter())
		}
	}
	return min(longest, maxRetryAfter)
}
