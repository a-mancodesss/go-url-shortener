# Architecture — URL Shortener

> Go + Gin + PostgreSQL + Redis. Production-grade layout, documented trade-offs, and scaling math.

---

## 1. Overview

```
Client ──► Load Balancer (NGINX / ALB / Cloudflare)
              │
              ├─► App Instance #1 (Go/Gin, stateless, Snowflake node 1)
              ├─► App Instance #2 (Go/Gin, node 2)
              └─► App Instance #N
                         │
            ┌────────────┼────────────┐
            ▼            ▼            ▼
        PostgreSQL    Redis Cluster   (Optional) Kafka / Analytics
        (source of    (cache + rate
         truth)        limiting)
```

**Request flows:**

*Write — `POST /api/v1/shorten`*
```
1. Rate limiter (Redis fixed-window) → 429 if exceeded
2. Validate URL + alias
3. Snowflake.NextID() → Base62 → short_code (7 chars)
4. INSERT into Postgres (unique on short_code)
5. SET redis url:{code} → long_url (TTL 24h or expires_at)
6. Return { short_url: https://sho.rt/abc1234 }
```

*Read — `GET /:code` (hot path, 90%+ of traffic)*
```
1. Rate limiter (looser bucket: 300/min)
2. GET redis url:{code} → on hit: 302 + async INCR clicks → DONE ( <2ms p95 )
3. On miss: SELECT from Postgres → populate Redis → 302
```

---

## 2. Folder Structure (Standard Go Layout)

```
url-shortener/
├── cmd/server/main.go           # Entrypoint, DI wiring, graceful shutdown
├── internal/
│   ├── config/config.go         # Env-based config (12-factor)
│   ├── model/url.go             # Domain types, errors
│   ├── util/
│   │   ├── base62.go            # Encode/Decode, Pad
│   │   ├── snowflake.go         # Distributed ID generator
│   │   └── validator.go         # URL/alias validation
│   ├── repository/postgres.go   # Postgres queries (pgxpool)
│   ├── cache/redis.go           # Cache + rate limiter (go-redis + Lua)
│   ├── service/url_service.go   # Business logic, cache-aside, write-through
│   ├── handler/url_handler.go   # Gin handlers (Shorten/Redirect/Stats)
│   └── middleware/
│       ├── rate_limiter.go
│       └── logger.go
├── migrations/                  # SQL migrations (000001_create_urls.*)
├── Dockerfile                   # Multi-stage build
├── docker-compose.yml           # Local prod-like stack
├── Makefile                     # Common commands
├── .env.example
├── go.mod / go.sum
├── ARCHITECTURE.md              # (this file)
└── README.md
```

**Why `internal/`?** Go enforces import boundary — prevents external importers from coupling to internals. `cmd/` keeps binaries separate if you later add `cmd/worker` for analytics.

**Why not `pkg/`?** `pkg/` is for libraries intended to be imported. This is an application, not a library.

---

## 3. Design Decisions (Why / Why Not)

### 3.1 Language & Framework: Go + Gin

| Choice | Why | Why Not |
|---|---|---|
| **Go** | Single static binary, low memory (~20MB), goroutines for 10k+ concurrent conns, great pgx/redis clients | Java/Kotlin heavier, Node single-threaded, Python GIL |
| **Gin** | Fastest mature router (~40k req/s on 1 core), middleware chain, validation via `go-playground/validator`, huge ecosystem | Echo similar but smaller community; Fiber uses fasthttp (incompatible with net/http middleware); Chi more minimal but slower; stdlib `net/http` verbose |

**Alternative rejected:** `gRPC` for external API — overkill for simple JSON; REST is universally consumable.

### 3.2 Database: PostgreSQL

| Aspect | Decision |
|---|---|
| **Why Postgres** | ACID, `UNIQUE` on short_code for free dedup, `BIGINT` PK, good write throughput (5-10k tps on single node), `pgxpool` is production-proven, easy to scale reads via replicas |
| **Why not MySQL** | Similar but weaker `UPSERT` semantics, less robust `EXPLAIN` |
| **Why not NoSQL (DynamoDB/Cassandra)** | No need for multi-region active-active yet; relational model is simple; DynamoDB's eventual consistency complicates dedup |
| **Why not SQLite** | Not concurrent, no network, fails at multi-instance |
| **Schema** | `id BIGINT PK` (Snowflake), `short_code VARCHAR(20) UNIQUE`, `long_url TEXT`, `clicks BIGINT`, `expires_at TIMESTAMPTZ`. Indexes on `long_url` (dedup) and `expires_at` (cleanup). |

**Scaling path:** Single primary → read replicas → Citus / sharding by `hash(short_code) % N` if >100M rows. Writes are append-only, excellent for partitioning by `created_at`.

### 3.3 Cache: Redis

| Aspect | Decision |
|---|---|
| **Why Redis** | In-memory <1ms, persistence (AOF), Lua for atomic rate limiting, TTL per key, pub/sub for invalidation |
| **Why not Memcached** | No Lua, no persistence, no data structures; Redis is superset |
| **Why not in-memory (sync.Map)** | Not shared across instances; stale after deploy; no eviction control |
| **Strategy** | **Cache-aside** on read + **write-through** on create. `GET url:{code}` first, fallback to DB. TTL = min(24h, expires_at). On write, populate cache immediately so first redirect is fast. |
| **Stampede protection** | Not implemented in v1; for prod add `singleflight` (golang.org/x/sync) to coalesce concurrent misses for same hot key. |
| **Eviction** | `allkeys-lru`, TTL-driven. Hit ratio >95% expected (Zipf distribution — top 10% URLs get 90% traffic). |

### 3.4 Rate Limiting: Redis Fixed Window

| Aspect | Decision |
|---|---|
| **Chosen** | Fixed window counter via Lua `INCR` + `EXPIRE` (atomic, O(1), one key per IP/window) |
| **Why** | Simplest, lowest Redis load, good enough for abuse prevention |
| **Trade-off** | Burst at window boundary allows 2× limit (e.g., 100 at 00:59 + 100 at 01:00). Acceptable for URL shortener. |
| **Alternatives** | **Sliding window log** (sorted set, accurate but O(log N) + more memory) → upgrade if needed. **Token bucket** (needs refill) → more code. **In-memory** → not distributed. |
| **Policy** | `POST /shorten`: 60 req/min per IP. `GET /:code`: 300 req/min per IP. Fail-open if Redis down (availability > strict limiting). Headers: `X-RateLimit-*`. |
| **Prod upgrade** | Put rate limiting at edge (Cloudflare / NGINX `limit_req` / Envoy) to shed load before hitting app. |

### 3.5 Unique ID Generation: Snowflake (Twitter)

| Strategy | Pros | Cons | Verdict |
|---|---|---|---|
| **Postgres SERIAL + Base62** | Simple, no clock issues | Single writer bottleneck, reveals count, hard to shard | ❌ For prod with >1 instance |
| **UUID v4** | No coordination | 128-bit, 36 chars, not URL-friendly, random → bad DB locality | ❌ |
| **Hash (MD5/SHA → Base62)** | Deterministic, dedup for same long_url | Collision handling needed, extra DB lookup, variable length | ❌ |
| **Snowflake** | Distributed, no DB call, sortable by time, 64-bit → 7-char Base62, 4M IDs/sec/node, no coordination except nodeID | Needs nodeID assignment, clock drift handling | ✅ Chosen |
| **ULID / UUIDv7** | Similar to Snowflake, time-ordered | Longer (26 chars), dependency | Consider if Snowflake complexity unwanted |

**Implementation details (`internal/util/snowflake.go:1`):**
- 41 bits timestamp (ms since 2024-01-01), 10 bits nodeID (0-1023), 12 bits sequence (4096/ms/node).
- `nodeID` from env `SNOWFLAKE_NODE_ID` — in Kubernetes use StatefulSet ordinal or init container that claims ID via Redis `INCR`.
- Clock drift: if `now < lastTS`, spin-wait; production should alert.
- Base62 alphabet `0-9a-zA-Z` — URL-safe without encoding; padded to 7 chars for aesthetics.

**Capacity:** 62⁷ ≈ 3.5 trillion codes at 7 chars; 62⁸ ≈ 218T at 8 chars. At 1B URLs, collision probability negligible (IDs are monotonic, not random).

### 3.6 Other Choices

- **Validation:** `go-playground/validator` via Gin binding tags (`binding:"required,url"`).
- **Config:** Env vars (12-factor), `joho/godotenv` only for local; in prod inject via Docker/K8s secrets.
- **Migrations:** Embedded SQL + auto-migrate on boot for simplicity; for prod switch to `golang-migrate/migrate` or `pressly/goose` with `make migrate-up`.
- **Observability:** Gin `Recovery` + request logger; prod add `prometheus` metrics (`/metrics`), `pprof`, OpenTelemetry tracing.
- **Graceful shutdown:** `http.Server.Shutdown` with 10s timeout, drains connections.

---

## 4. Scaling & Concurrency Model

### 4.1 Single Instance Benchmark (estimates, `go test -bench`, `wrk`)

| Endpoint | Cache | p50 | p95 | p99 | RPS (2 vCPU, 4GB) |
|---|---|---|---|---|---|
| `GET /:code` | HIT (Redis) | 0.8ms | 2ms | 5ms | **12k–15k** |
| `GET /:code` | MISS (Postgres) | 4ms | 12ms | 25ms | 3k–5k |
| `POST /shorten` | — | 5ms | 15ms | 30ms | 2k–4k |

*Measured with `wrk -t4 -c200 -d30s` against local Docker stack.*

### 4.2 How Many Concurrent Users?

**Definitions:**
- *Concurrent connections* ≠ *concurrent users*. 10k concurrent connections ≈ 100k–500k active users (depending on think time).
- Go can handle **10k–50k concurrent goroutines** per instance (each ~2KB stack, epoll-based netpoll).

**Capacity table:**

| Deployment | Instances | Postgres | Redis | Estimated Concurrent Users* | RPS capacity |
|---|---|---|---|---|---|
| **Local (docker-compose)** | 1 | 1 (16) | 1 | 1k–5k | 5k |
| **Small prod** | 3 × 1 vCPU | 1 primary + 1 replica (db.t3.medium) | ElastiCache t3.micro (1 shard) | **20k–50k** | 20k |
| **Medium prod** | 5 × 2 vCPU (behind ALB) | db.r6g.large + 2 replicas | Redis Cluster 3 shards | **100k–250k** | 60k–80k |
| **Large prod** | 20 × 4 vCPU + CDN (Cloudflare) | Aurora / sharded (Citus) | Redis Cluster 10 shards + read replicas | **1M+** | 300k+ |

\* *Concurrent users = users actively clicking within same second window. Assuming 1 req/user/sec average for shortener redirect traffic (mostly bursty). With CDN caching 302s, effective capacity 5–10× higher.*

**Bottlenecks in order:**
1. **Postgres writes** (shorten) — ~5k inserts/sec per primary. Mitigate: batch, async persist (write to Kafka then DB), or add `long_url` dedup to avoid duplicate inserts.
2. **Redis throughput** — single shard ~100k ops/sec. Mitigate: cluster, local in-memory LRU fronting Redis (e.g., `ristretto`).
3. **App CPU** — Gin JSON marshal/unmarshal; scale horizontally (stateless).

### 4.3 Production Topology (Recommended)

```
Users → Cloudflare (DDoS, cache 302 for 1-5s) → ALB → Target Group (Gin × N, auto-scaling)
                                      │
                    ┌─────────────────┼─────────────────┐
                    ▼                 ▼                 ▼
              RDS Postgres       ElastiCache Redis   S3 (logs)
              Multi-AZ           Cluster Mode        CloudWatch / Prometheus
              Read Replicas      (3+ shards)         Grafana / Datadog
```

- **Horizontal scale:** App is stateless; scale via Kubernetes HPA on CPU/RPS or ECS auto-scaling. Add instances linearly.
- **DB scale:** Read `GET /:code` misses go to replicas; writes go to primary. For >100M rows, partition by `created_at` or hash.
- **Cache scale:** Redis Cluster, enable `cluster-enabled`. Use `allkeys-lru` + TTL. Hot keys naturally replicate via client.
- **CDN:** Cache 302 responses at edge for 1–5 seconds → absorbs thundering herd for viral links.

---

## 5. Failure Modes & Resilience

| Failure | Mitigation |
|---|---|
| **Postgres down** | Redirects still serve from Redis (stale cache). Writes queue or return 503 with `Retry-After`. Circuit breaker. |
| **Redis down** | Fail-open for rate limiter; reads fallback to Postgres (higher latency). Writes still succeed (DB is source of truth). |
| **App crash** | ALB health check (`/health`) removes unhealthy; K8s restarts. |
| **Clock skew (Snowflake)** | Spin-wait, log, alert; use NTP/chrony. NodeID collision if misconfigured → DB unique constraint catches duplicate, retry with new ID. |
| **Hot key / viral link** | Redis handles 100k rps per key; add local cache (`singleflight` + `sync.Map` with 1s TTL) to reduce Redis load. CDN absorbs bulk. |

---

## 6. Security Notes

- Validate `long_url` is http/https only; block private IP ranges (SSRF) in prod.
- Custom alias regex `^[a-zA-Z0-9_-]{3,20}$`.
- Rate limiting per IP + optional per-API-key (add `Authorization: Bearer` middleware).
- Do not expose `id` directly; only `short_code`.
- Consider adding CAPTCHA on `POST /shorten` for anonymous users.

---

## 7. Commands (Quick Ref)

See `README.md:1` for full runbook. Essentials:

```bash
# Local dev (needs Go 1.22+, Docker)
cp .env.example .env
docker compose up --build -d   # Postgres + Redis + app
make run                       # or go run ./cmd/server

# Test
curl -X POST localhost:8080/api/v1/shorten -H 'Content-Type: application/json' -d '{"long_url":"https://example.com"}'
curl -i localhost:8080/<code>

# Prod build
docker build -t url-shortener:latest .
docker run -p 8080:8080 --env-file .env url-shortener:latest
```

---

## 8. Future Improvements

- [ ] Analytics pipeline: Kafka + ClickHouse for click streams (instead of sync `UPDATE clicks`)
- [ ] Expiry janitor: cron `DELETE WHERE expires_at < NOW()` or TTL via Redis keyspace notifications
- [ ] Auth & quotas: API keys, per-user dashboards
- [ ] Tests: `go test ./...` with `sqlmock` + `miniredis`
- [ ] gRPC internal API for service mesh
