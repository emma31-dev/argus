// Package alert implements Argus's alerting core: the pure debounce state
// machine (Step), the Engine that persists state and incidents, the alert
// queue (Redis or in-memory), the Dispatcher that delivers alerts with
// retries and dead-lettering, and the Telegram and webhook notifiers.
//
// The package depends only on small interfaces (StateStore, IncidentStore,
// Outbox, TxRunner, Queue, SubscriptionStore, TokenLister) so it can be tested
// with fakes (MemStore) and never imports the Postgres store.
//
// Atomicity: for every token with a transition, the Engine writes the
// incident, the new state and the alerts to the Outbox in ONE transaction
// (TxRunner). A failure rolls all of it back and the transition re-fires on
// the next round, so incidents and states never diverge. A Relay then moves
// committed outbox rows to the Queue in order, at-least-once.
//
// Queue design: alerts are JSON-encoded onto a Redis LIST. Producers LPUSH to
// "<prefix>:queue"; the dispatcher atomically moves the oldest item to
// "<prefix>:processing" with BLMOVE (reliable-queue pattern), delivers it,
// then removes it from the processing list (Ack) or moves it to
// "<prefix>:dead" (dead letter). On start the dispatcher moves anything left
// in the processing list back onto the queue, so a crash mid-delivery means
// at-least-once delivery. Notifiers de-duplicate recent sends so retries do
// not double-message recipients.
package alert
