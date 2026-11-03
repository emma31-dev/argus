package alert

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisQueue(t *testing.T) {
	url := os.Getenv("ARGUS_TEST_REDIS_URL")
	if url == "" {
		t.Skip("ARGUS_TEST_REDIS_URL not set")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()
	ctx := context.Background()
	prefix := "argus:test:" + time.Now().Format("150405.000000000")
	defer func() {
		rdb.Del(ctx, prefix+":queue", prefix+":processing", prefix+":dead")
	}()
	q := NewRedisQueue(rdb, prefix)

	if d, err := q.Dequeue(ctx, 100*time.Millisecond); d != nil || err != nil {
		t.Fatalf("empty queue: %v %v", d, err)
	}
	a1, a2 := sampleAlert(TurnedRed), sampleAlert(Recovered)
	for _, a := range []Alert{a1, a2} {
		if err := q.Enqueue(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	d1, err := q.Dequeue(ctx, time.Second)
	if err != nil || d1 == nil || d1.Alert.Kind != TurnedRed || d1.Alert.Gap != a1.Gap || !d1.Alert.At.Equal(a1.At) {
		t.Fatalf("FIFO broken: %+v %v", d1, err)
	}
	// Crash simulation: d1 stays in processing; Recover puts it back first in line.
	if n, err := q.Recover(ctx); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	d1, _ = q.Dequeue(ctx, time.Second)
	if d1.Alert.Kind != TurnedRed {
		t.Fatalf("recovered alert not first: %v", d1.Alert.Kind)
	}
	if err := q.Ack(ctx, d1); err != nil {
		t.Fatal(err)
	}
	d2, _ := q.Dequeue(ctx, time.Second)
	if d2.Alert.Kind != Recovered {
		t.Fatal(d2.Alert.Kind)
	}
	if err := q.Dead(ctx, d2, "gave up"); err != nil {
		t.Fatal(err)
	}
	if n, _ := rdb.LLen(ctx, prefix+":processing").Result(); n != 0 {
		t.Fatalf("processing list not empty: %d", n)
	}
	dead, err := q.DeadLetters(ctx, 10)
	if err != nil || len(dead) != 1 || dead[0].Reason != "gave up" || dead[0].Alert.Kind != Recovered {
		t.Fatalf("%+v %v", dead, err)
	}
	if n, _ := q.Len(ctx); n != 0 {
		t.Fatal(n)
	}

	// Poison payload is dead-lettered instead of wedging the queue.
	rdb.LPush(ctx, prefix+":queue", "not json")
	if d, err := q.Dequeue(ctx, time.Second); d != nil || err == nil {
		t.Fatalf("poison: %v %v", d, err)
	}
	if dead, _ := q.DeadLetters(ctx, 10); len(dead) != 2 {
		t.Fatalf("poison not dead-lettered: %d", len(dead))
	}
}
