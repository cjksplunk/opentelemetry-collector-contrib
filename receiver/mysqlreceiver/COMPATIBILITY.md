# Version Compatibility

This document lists every version-gated capability in the MySQL receiver. Version detection runs once at `Connect()` time via `fetchDBVersion()`. If detection fails, all predicates return `false`, which falls back to MySQL <8 behavior.

## Capability Predicates

| Predicate | Minimum Version | Fallback Behavior |
|---|---|---|
| `supportsQuerySampleText()` | MySQL 8.0+ | Top-query scraper omits `EXPLAIN`; `querySampleNoUserVars.tmpl` is used for SQL text |
| `supportsUserVariablesByThread()` | MySQL 5.7.3+ / MariaDB 10.5.2+ | `querySampleNoUserVars.tmpl` is used; `@traceparent` extraction is disabled; trace propagation is not available |
| `supportsReplicaStatus()` | MySQL 8.0.22+ | `SHOW SLAVE STATUS` is used instead of `SHOW REPLICA STATUS` |
| `supportsProcesslist()` | MySQL 8.0.22+ | `client.port` and `network.peer.port` remain `0`; `information_schema.PROCESSLIST` is **not** used as a fallback (it holds a global mutex, was deprecated in MySQL 8.0, removed in MySQL 9.0, and has already been removed from this receiver) |

## Timer Wait Tiers (`querySample.tmpl`)

Query sample duration is resolved in order. The first tier that produces a value is used.

| Tier | Source | Availability |
|---|---|---|
| 1 | Exact `TIMER_WAIT` for completed waits | All supported versions |
| 2 | PS timer approximation for in-progress waits | MySQL 5.7+ / 8.0+; **not used for MariaDB** — MariaDB's `statement.TIMER_WAIT` is updated only at yield points, not continuously, making it unreliable for in-progress statements |
| 3 | `thread.processlist_time` integer-second fallback | MySQL 5.7+, all MariaDB 10.x |

## Version Detection

- Detection runs **once** at `Connect()` time via `fetchDBVersion()`.
- Failure is **non-fatal**: `dbVersion` stays at its zero value and `Connect()` returns `nil`. All capability predicates return `false`, which produces MySQL <8 fallback behavior.
- The `dbVersion` struct holds two fields: `product` (MySQL or MariaDB) and `version` (semver).
- Connection errors surface on the first scrape, not at connect time.
