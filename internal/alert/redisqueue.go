package alert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisDeadMax = 1000 // dead-letter list is trimmed to this many entries
	// RedisDefaultPrefix is the key prefix used when none is given.
	RedisDefaultPrefix = "argus:alerts"
)

// RedisQueue is a reliable Queue on Redis LISTs: LPUSH to <prefix>:queue,
// BLMOVE (oldest first) to <prefix>:processing on Dequeue, LREM on Ack, and
// <prefix>:dead for dead letters. See the package documentation.
type RedisQueue struct {
	rdb   redis.Cmdable
	queue string
	proc  string
	dead  string
}

// NewRedisQueue returns a RedisQueue using the given key prefix
// (RedisDefaultPrefix if empty).
func NewRedisQueue(rdb redis.Cmdable, prefix string) *RedisQueue {
	if prefix == "" {
		prefix = RedisDefaultPrefix
	}
	return &RedisQueue{rdb: rdb, queue: prefix + ":queue", proc: prefix + ":processing", dead: prefix + ":dead"}
}

// Enqueue implements Queue.
func (q *RedisQueue) Enqueue(ctx context.Context, a Alert) error {
	raw, err := encodeAlert(a)
	if err != nil {
		return err
	}
	return q.rdb.LPush(ctx, q.queue, raw).Err()
}

// Dequeue implements Queue. wait is rounded up to whole seconds only when it
// is at least a second; Redis BLMOVE takes a float timeout.
func (q *RedisQueue) Dequeue(ctx context.Context, wait time.Duration) (*Delivery, error) {
	var raw string
	var err error
	if wait <= 0 {
		raw, err = q.rdb.LMove(ctx, q.queue, q.proc, "RIGHT", "LEFT").Result()
	} else {
		raw, err = q.rdb.BLMove(ctx, q.queue, q.proc, "RIGHT", "LEFT", wait).Result()
	}
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a Alert
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		// Poison message: dead-letter it so it cannot wedge the queue.
		d := &Delivery{raw: raw}
		_ = q.Dead(ctx, d, "undecodable payload: "+err.Error())
		return nil, fmt.Errorf("alert queue: undecodable payload dead-lettered: %w", err)
	}
	return &Delivery{Alert: a, raw: raw}, nil
}

// Ack implements Queue.
func (q *RedisQueue) Ack(ctx context.Context, d *Delivery) error {
	return q.rdb.LRem(ctx, q.proc, 1, d.raw).Err()
}

// Dead implements Queue.
func (q *RedisQueue) Dead(ctx context.Context, d *Delivery, reason string) error {
	b, err := json.Marshal(DeadAlert{Alert: d.Alert, Reason: reason, At: time.Now().UTC()})
	if err != nil {
		return err
	}
	pipe := q.rdb.TxPipeline()
	pipe.LPush(ctx, q.dead, string(b))
	pipe.LTrim(ctx, q.dead, 0, redisDeadMax-1)
	pipe.LRem(ctx, q.proc, 1, d.raw)
	_, err = pipe.Exec(ctx)
	return err
}

// RequeueDead implements Queue. The dead list is newest-first (LPUSH), so it
// is scanned from the tail to requeue the oldest eligible alerts first. Each
// move is one MULTI/EXEC (LPUSH to the queue, LREM from the dead list), so an
// alert is never lost between the lists.
func (q *RedisQueue) RequeueDead(ctx context.Context, max int) (int, error) {
	items, err := q.rdb.LRange(ctx, q.dead, 0, -1).Result()
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-DeadRequeueMaxAge)
	n := 0
	for i := len(items) - 1; i >= 0 && n < max; i-- {
		var d DeadAlert
		if json.Unmarshal([]byte(items[i]), &d) != nil || d.At.Before(cutoff) {
			continue
		}
		raw, err := encodeAlert(d.Alert)
		if err != nil {
			continue
		}
		pipe := q.rdb.TxPipeline()
		pipe.LPush(ctx, q.queue, raw)
		pipe.LRem(ctx, q.dead, 1, items[i])
		if _, err := pipe.Exec(ctx); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Recover implements Queue. It must only be called when no other consumer is
// running (single dispatcher).
func (q *RedisQueue) Recover(ctx context.Context) (int, error) {
	n := 0
	for {
		_, err := q.rdb.LMove(ctx, q.proc, q.queue, "LEFT", "RIGHT").Result()
		if errors.Is(err, redis.Nil) {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		n++
	}
}

// DeadLetters returns up to n most recent dead-lettered alerts.
func (q *RedisQueue) DeadLetters(ctx context.Context, n int64) ([]DeadAlert, error) {
	items, err := q.rdb.LRange(ctx, q.dead, 0, n-1).Result()
	if err != nil {
		return nil, err
	}
	out := make([]DeadAlert, 0, len(items))
	for _, it := range items {
		var d DeadAlert
		if json.Unmarshal([]byte(it), &d) == nil {
			out = append(out, d)
		}
	}
	return out, nil
}

// Len returns the number of pending alerts.
func (q *RedisQueue) Len(ctx context.Context) (int64, error) {
	return q.rdb.LLen(ctx, q.queue).Result()
}
