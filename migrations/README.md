# migrations

SQL migrations embedded into the Go binary and applied by [`../internal/store/store.go`](../internal/store/store.go) in file-name order, each in its own transaction. Applied versions are recorded in `schema_migrations`.

- [`0001_init.sql`](0001_init.sql): tokens, snapshots, incidents and the core indexes.
- [`0002_kv.sql`](0002_kv.sql): the key/value table used for discovery state.
- [`0003_outbox_staleness_events.sql`](0003_outbox_staleness_events.sql): the alert outbox, staleness tracking and incident events.

The runner can be found in [`../internal/store/store.go`](../internal/store/store.go). It is called on startup by the monitor.
