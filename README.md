# URL Shortener — Go + Gin + PostgreSQL + Redis

Production-grade URL shortener with **caching**, **rate limiting**, and **distributed ID generation**. Stateless, horizontally scalable, Dockerized.

**Stack:** Go 1.27 · Gin · PostgreSQL 16 (pgxpool) · Redis 7 (go-redis) · Snowflake IDs · Base62

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


| Var                                   | Default                                        | Description                        |
| ------------------------------------- | ---------------------------------------------- | ---------------------------------- |
| `PORT`                                | 8080                                           | HTTP port                          |
| `BASE_URL`                            | [http://localhost:8080](http://localhost:8080) | Used to build `short_url`          |
| `POSTGRES_HOST/PORT/USER/PASSWORD/DB` | localhost:5432/postgres                        | DB connection                      |
| `REDIS_ADDR`                          | localhost:6379                                 | Redis address                      |
| `REDIS_TTL`                           | 24h                                            | Cache TTL                          |
| `SNOWFLAKE_NODE_ID`                   | 1                                              | 0–1023, unique per instance        |
| `APP_ENV`                             | development                                    | `production` sets Gin release mode |
| `CORS_ALLOWED_ORIGINS`                | `http://localhost:5173`, `http://127.0.0.1:5173`, `http://localhost:4173`, `http://127.0.0.1:4173`, `https://*.vercel.app` | Comma-separated frontend origins allowed by CORS. Supports wildcard subdomains. Use `*` for any origin in local development only. |

### CORS

Browser clients must come from an origin listed in `CORS_ALLOWED_ORIGINS` (scheme + host + port). The defaults cover the Vite dev server on `:5173`, the preview server on `:4173`, and any subdomain of `vercel.app` (for Vercel preview/production deployments).

```bash
# Production example
CORS_ALLOWED_ORIGINS=https://app.example.com,https://staging.example.com

# Allow any Vercel deployment (e.g. my-app-git-main-user.vercel.app)
CORS_ALLOWED_ORIGINS=https://*.vercel.app
```

Wildcards match subdomains only: `https://*.vercel.app` allows `https://my-app.vercel.app` but not `https://vercel.app` or lookalikes such as `https://vercel.app.evil.com`. Preflight `OPTIONS` requests are answered before the rate limiter, and `X-RateLimit-*` headers are exposed to JavaScript. Requests without an `Origin` header (curl, server-to-server) are unaffected.


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

