# URL Shortener — Go + Gin + PostgreSQL + Redis

Production-grade URL shortener with **caching**, **rate limiting**, and **distributed ID generation**. Stateless, horizontally scalable, Dockerized.

**Stack:** Go 1.22 · Gin · PostgreSQL 16 (pgxpool) · Redis 7 (go-redis) · Snowflake IDs · Base62

---

## 1. Features

- `POST /api/v1/shorten` — create short URL (auto Base62 or custom alias, optional expiry)
- `GET /:code` — 302 redirect (cache-aside: Redis → Postgres)
- `GET /api/v1/stats/:code` — click count & metadata
- `GET /health` — liveness probe
- Rate limiting (Redis fixed-window, per-IP, fail-open)
- Snowflake IDs (4096 IDs/ms/node, 1024 nodes, sortable)
- Graceful shutdown, Docker multi-stage build, `docker-compose` prod-like stack

---

## 2. Architecture

See **[ARCHITECTURE.md](./ARCHITECTURE.md)** for deep dive: design decisions (why Gin, why Snowflake vs UUID/serial, why Redis vs Memcached), scaling math, bottlenecks, and production topology.

**TL;DR diagram:**
```
POST /shorten → Rate Limiter → Snowflake → Base62 → Postgres → Redis (write-through)
GET /:code    → Rate Limiter → Redis (hit? 302) → Postgres (miss) → Redis populate → 302
```

---

## 3. Project Structure

```
cmd/server/main.go           # Entrypoint, DI, graceful shutdown
internal/
  config/                    # 12-factor env config
  model/                     # Domain types
  util/                      # Base62, Snowflake, validator
  repository/                # Postgres (pgxpool)
  cache/                     # Redis cache + rate limiter
  service/                   # Business logic
  handler/                   # Gin handlers
  middleware/                # Rate limiter, logger
migrations/                  # SQL
Dockerfile / docker-compose.yml / Makefile
```

---

## 4. Quick Start

### Prerequisites

- Go 1.22+
- Docker & Docker Compose

### A. Run with Docker (recommended — one command)

```bash
git clone <repo> && cd url-shortener
cp .env.example .env          # edit if needed

# Starts app + Postgres + Redis
docker compose up --build -d

# Check health
curl http://localhost:8080/health
# {"status":"ok"}

# Logs
docker compose logs -f app

# Stop
docker compose down -v
```

### B. Run locally without Docker (needs Postgres + Redis installed)

```bash
cp .env.example .env
# Ensure Postgres and Redis are running and .env points to them
go mod tidy
go run ./cmd/server
# Server on :8080
```

### C. Makefile shortcuts

```bash
make run          # go run ./cmd/server
make build        # builds bin/server
make vet          # go vet ./...
make test         # go test ./...
make docker-up    # docker compose up --build -d
make docker-down  # docker compose down -v
make logs         # docker compose logs -f app
```

---

## 5. API Usage

### Create short URL

```bash
curl -X POST http://localhost:8080/api/v1/shorten \
  -H "Content-Type: application/json" \
  -d '{"long_url":"https://example.com/very/long/path"}'

# Response 201
# {"short_code":"1a2B3c4","short_url":"http://localhost:8080/1a2B3c4","long_url":"https://example.com/very/long/path"}
```

With custom alias + expiry:

```bash
curl -X POST http://localhost:8080/api/v1/shorten \
  -H "Content-Type: application/json" \
  -d '{"long_url":"https://example.com","custom_alias":"my-link","expires_at":"2026-12-31T00:00:00Z"}'
```

### Redirect

```bash
curl -i http://localhost:8080/1a2B3c4
# HTTP/1.1 302 Found
# Location: https://example.com/very/long/path
```

### Stats

```bash
curl http://localhost:8080/api/v1/stats/1a2B3c4
# {"id":123456789,"short_code":"1a2B3c4","long_url":"https://example.com/...","clicks":42,"created_at":"..."}
```

### Rate limit headers

```
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 42
X-RateLimit-Reset: 38
# 429 Too Many Requests when exceeded
```

---

## 6. Configuration

All via env vars (see `.env.example:1`):

| Var | Default | Description |
|---|---|---|
| `PORT` | 8080 | HTTP port |
| `BASE_URL` | http://localhost:8080 | Used to build `short_url` |
| `POSTGRES_HOST/PORT/USER/PASSWORD/DB` | localhost:5432/postgres | DB connection |
| `REDIS_ADDR` | localhost:6379 | Redis address |
| `REDIS_TTL` | 24h | Cache TTL |
| `SNOWFLAKE_NODE_ID` | 1 | 0–1023, unique per instance |
| `APP_ENV` | development | `production` sets Gin release mode |

---

## 7. Production Deployment

### Build & push

```bash
docker build -t ghcr.io/yourorg/url-shortener:latest .
docker push ghcr.io/yourorg/url-shortener:latest
```

### Run (single host)

```bash
docker run -d -p 8080:8080 --env-file .env ghcr.io/yourorg/url-shortener:latest
```

### Kubernetes (sketch)

```yaml
# Deployment: 3 replicas, each with unique SNOWFLAKE_NODE_ID via StatefulSet ordinal or init
env:
  - name: SNOWFLAKE_NODE_ID
    valueFrom:
      fieldRef: { fieldPath: metadata.name } # parse ordinal
  - name: POSTGRES_HOST
    value: rds-postgres.internal
  - name: REDIS_ADDR
    value: elasticache-redis:6379
livenessProbe: { httpGet: { path: /health, port: 8080 } }
readinessProbe: { httpGet: { path: /health, port: 8080 } }
# HPA on CPU / RPS
# ALB/Ingress → Service
```

**Recommended prod topology:** Cloudflare (DDoS + cache 302 1–5s) → ALB → Gin (3–20 pods, HPA) → RDS Postgres (Multi-AZ + replicas) + ElastiCache Redis Cluster (3+ shards). See `ARCHITECTURE.md:1` for details.

---

## 8. Concurrency & Scaling (Concise)

| Deployment | Instances | RPS capacity | Concurrent users* |
|---|---|---|---|
| **Local (1 container)** | 1×1vCPU | ~5k | 1k–5k |
| **Small prod** | 3×1vCPU + 1 Postgres + 1 Redis | ~20k | **20k–50k** |
| **Medium prod** | 5×2vCPU + PG replica + Redis cluster 3 shards | ~80k | **100k–250k** |
| **Large + CDN** | 20×4vCPU + sharded DB + 10 Redis shards + Cloudflare | ~300k | **1M+** |

\* Users actively clicking within same second; with CDN caching 302s, effective 5–10× higher. App is stateless → scale linearly. Bottleneck order: Postgres writes → Redis → CPU. See `ARCHITECTURE.md:1` for full math and `wrk` benchmarks.

**Quick load test:**

```bash
# Install wrk
wrk -t4 -c200 -d30s http://localhost:8080/<code>
# shorten throughput
ab -n 10000 -c 100 -p body.json -T application/json http://localhost:8080/api/v1/shorten
```

---

## 9. ID Generation, Caching, Rate Limiting (Why)

- **Snowflake + Base62** — distributed, no DB bottleneck, 7-char URL-safe codes, 3.5T combos at 7 chars. Alternatives (serial/UUID/hash) rejected for scaling or length reasons — see `ARCHITECTURE.md:1`.
- **Redis cache-aside + write-through** — 95%+ hit ratio (Zipf), <2ms p95 on hit. Fallback to Postgres on miss.
- **Redis fixed-window limiter** — O(1) Lua `INCR+EXPIRE`, per-IP, fail-open if Redis down. Upgrade to sliding window or edge (NGINX/Cloudflare) if needed.

---

## 10. Development Notes

```bash
go vet ./...          # lint
go test ./...         # tests (add sqlmock/miniredis for unit)
golangci-lint run     # if installed
```

Migrations run automatically on boot (embedded SQL). For manual:

```bash
psql $DATABASE_URL -f migrations/000001_create_urls.up.sql
```

---

## 11. License

MIT
