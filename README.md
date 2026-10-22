# remit-platform

A cross-border money transfer backend: wallets, double-entry ledger, FX conversion, netting and settlement,
and reconciliation, built as small services that talk over gRPC, HTTP and a message queue.

It is a portfolio project. The money is simulated, but the engineering problems are the real ones in payments:
**never lose or duplicate money, stay correct under concurrency and retries, and be able to prove it.**

## Why this exists

Payments backends are mostly about correctness under failure, not about CRUD. I built this to show how I approach that:

- **Correctness is enforced by the database, not by hope.** Balances can't go negative (a `CHECK` constraint plus a
  row-locked balance check), ledger entries are append-only, and every transfer is a single ACID transaction.
- **Retries are safe.** Every transfer carries an idempotency key. Replays return the original result, and reusing a
  key with a different payload is rejected, even when 20 identical requests arrive at the same moment.
- **The system can check itself.** A reconciliation job proves that balances equal the sum of entries, that every
  transaction balances to zero in each currency, and that settlements match their transactions.
- **Decisions are written down.** The trade-offs are in this file, and the one benchmark I ran, including its
  failure, is documented in [`docs/load-test.md`](docs/load-test.md).

## What it does

```
Alice (USD wallet) --- $10 --->  [ledger]  --->  Bob (NGN wallet) receives 15,000 NGN
                                    |
        FX rate stored with the transaction (rate, source, timestamp)
                                    |
        netted per currency corridor and settled by a worker
```

- Create wallets in any supported currency (USD, NGN, EUR, GBP in the dev data).
- Transfer between wallets, same-currency or cross-currency, with idempotency keys.
- Cross-currency transfers are posted as four double-entry lines through per-currency system FX wallets, so every
  currency balances on its own.
- A worker nets posted cross-currency transfers per corridor, creates settlements, and sends them to a (simulated) rail.
- A reconciliation job audits the ledger. An export script produces a CSV or JSON audit trail with the FX details.

## Status

| Area | State |
|---|---|
| Services, database, queue, auth, rate limiting, metrics, logging | Built and verified locally and in the end-to-end test |
| Tests: unit and database tests, concurrency tests, end-to-end test, load test | Built; load test did not meet its targets (see [Performance](#performance)) |
| Container images and production Docker Compose stack | Built and verified locally |
| Terraform for AWS (VPC, EC2, RDS, SQS, ECR, IAM) | **Written but not yet applied or validated against a real AWS account** |
| CI/CD deploy to EC2, runtime Secrets Manager integration | **Not built yet** |

---

## System architecture

How the services talk to each other:

```mermaid
flowchart LR
    client([API client])

    subgraph stack ["Docker Compose network"]
        gw["API Gateway<br/>Node.js, Fastify"]
        ledger["Ledger Service<br/>Go, gRPC"]
        fx["FX Service<br/>Node.js, Fastify"]
        settle["Settlement Worker<br/>Go"]
        recon["Reconciliation<br/>Go"]
        redis[("Redis")]
    end

    pg[("PostgreSQL")]
    sqs[["SQS queue"]]
    rail(["Payment rail<br/>simulated"])

    client -->|"HTTP, X-API-Key"| gw
    gw -->|"gRPC with mTLS"| ledger
    gw -->|"HTTP"| fx
    gw -->|"rate-limit counters"| redis
    gw -->|"API key lookup, read only"| pg
    ledger -->|"HTTP, rate lookup"| fx
    ledger -->|"SQL: transfers and outbox"| pg
    ledger -->|"outbox relay"| sqs
    fx -->|"cache"| redis
    fx -->|"rates"| pg
    settle -->|"netting and settlement"| pg
    settle -->|"send settlement"| rail
    recon -->|"integrity checks, writes results"| pg
```

| Service | Language | Responsibility |
|---|---|---|
| **api-gateway** | Node.js, TypeScript, Fastify | Public REST API. API-key authentication, per-key rate limiting, request validation, correlation ids, Prometheus metrics. Translates REST to gRPC. |
| **ledger** | Go | The only writer of balances and ledger entries. gRPC API. Atomic transfers, idempotency, double-entry postings, outbox events and the relay to SQS. |
| **fx-service** | Node.js, TypeScript, Fastify | Fetches rates from a provider (a static file in dev), stores history in Postgres, serves the latest rate from a Redis cache. |
| **settlement-worker** | Go | Nets posted cross-currency transfers per corridor, creates settlements, sends them to a rail. |
| **reconciliation** | Go | Verifies the ledger's invariants and records the results. Runs daily, or on demand with `-once`. |

### Life of a transfer

```mermaid
sequenceDiagram
    participant C as Client
    participant G as Gateway
    participant L as Ledger
    participant F as FX Service
    participant P as Postgres
    participant Q as SQS

    C->>G: POST /v1/transfers (X-API-Key, Idempotency-Key)
    G->>G: check API key, rate limit, validate body
    G->>L: Transfer over gRPC with mTLS and correlation id
    L->>P: read both wallets (currencies)
    opt wallets use different currencies
        L->>F: GET /rates
        F-->>L: rate, source, fetched_at
    end
    L->>P: one transaction: lock key and wallets, move money, write entries and outbox event
    L-->>G: transaction
    G-->>C: 201 Created
    Note over L,Q: asynchronously, the outbox relay publishes the event
    L->>Q: transaction.posted
    Note over P: later, the settlement worker nets and settles it
```

The rate is fetched before the database transaction opens, so no row locks are held during an HTTP call.

### Data model

Nine tables in [`sql/migrations/0001_initial.sql`](sql/migrations/0001_initial.sql):

| Table | Purpose |
|---|---|
| `wallets` | One per owner and currency. Holds the current `balance` (never negative for user wallets). `is_system` marks the per-currency FX wallets. |
| `transactions` | One per transfer. Stores amount, converted amount, the **FX rate, source and timestamp used**, corridor, status and correlation id. |
| `ledger_entries` | Append-only double-entry lines. Never updated or deleted. |
| `idempotency_keys` | Key, request hash and the resulting transaction. |
| `outbox_events` | Events written in the same transaction as the transfer, published to SQS afterwards. |
| `settlements` | Netted amounts per corridor and their status. |
| `fx_rates` | Rate history. |
| `reconciliation_logs` | Results of every reconciliation run. |
| `api_keys` | SHA-256 hashes of API keys. The raw key is shown once at creation. |

---

## Infrastructure architecture

How the AWS deployment is laid out, and how the EC2 host reaches the queue, the database and the image registry
(defined in [`infra/terraform`](infra/terraform); see [Status](#status) for what has been applied).

```mermaid
flowchart TB
    internet((Internet))
    operator([Operator])

    subgraph aws ["AWS, one region"]
        ecr["ECR<br/>5 image repositories"]
        sqs[["SQS: remit-events<br/>plus dead-letter queue"]]
        sm["Secrets Manager<br/>RDS master password"]
        ssm["SSM Session Manager"]

        subgraph vpc ["VPC 10.0.0.0/16"]
            igw["Internet gateway"]

            subgraph pub ["Public subnet, security group: port 3000 only"]
                subgraph host ["EC2, Amazon Linux 2023, Docker Compose, instance role"]
                    gwc["api-gateway :3000"]
                    ledgerc["ledger"]
                    fxc["fx-service"]
                    workers["settlement-worker<br/>reconciliation"]
                    redisc[("redis")]
                    engine["Docker engine"]
                end
            end

            subgraph priv ["Private subnets in 2 AZs, no internet route"]
                rds[("RDS PostgreSQL 15<br/>encrypted, not public")]
            end
        end
    end

    internet -->|"HTTP :3000"| igw --> gwc
    gwc -->|"gRPC, mTLS"| ledgerc
    gwc --> fxc
    ledgerc --> fxc
    gwc --> redisc
    fxc --> redisc
    ledgerc -->|"5432, TLS, only from the app security group"| rds
    fxc --> rds
    workers --> rds
    gwc -->|"API key lookup"| rds
    ledgerc -->|"HTTPS, instance role credentials"| sqs
    engine -.->|"pull images"| ecr
    engine -.->|"read DB password"| sm
    operator -->|"shell, no SSH, no open port 22"| ssm -.-> host
```

**How the pieces connect:**

- **EC2 to the queue.** The `ledger` container publishes outbox events to SQS over HTTPS. The EC2 instance has an IAM
  role that allows only `SendMessage`, `GetQueueUrl` and `GetQueueAttributes` on that one queue. No access keys are
  stored anywhere. The instance metadata hop limit is set to 2 so containers (one network hop further than the host)
  can reach the role's credentials. The queue has a dead-letter queue after 5 failed receives.
- **EC2 to the database.** RDS sits in private subnets with no internet route. Its security group accepts port 5432
  only from the app host's security group, and connections use TLS. RDS generates the master password and keeps it in
  Secrets Manager, so it is not in Terraform state or the repo.
- **EC2 to the image registry.** The role may pull images from the five ECR repositories (`remit/<service>`).
- **Operator access.** There is no SSH and no inbound port 22. Shell access is through SSM Session Manager.
- **Cost.** There is no NAT gateway: the host has a public IP in a public subnet and reaches AWS through the internet
  gateway. A small instance plus a `db.t4g.micro` keeps the bill low.

The same stack runs locally with [`docker-compose.prod.yml`](docker-compose.prod.yml), which reads its settings from an
env file, so the local run and the AWS run differ only in values (endpoints, credentials, image registry).

---

## Quick start

Prerequisites: Docker with Compose v2, `psql`, `curl`, `openssl`, `python3`. For development on the services:
Go 1.25 or newer and Node 20 or newer. The generated gRPC code is committed, so `protoc` is only needed if you change
`ledger.proto`.

### Option 1: one command (recommended)

```bash
bash scripts/e2e.sh
```

This builds every image, starts the full stack on a throwaway database, sends real requests through the gateway and
checks the results in Postgres (transfers, idempotency, error codes, API keys, rate limiting, the outbox, settlement,
reconciliation and the audit export). It ends with `E2E PASSED` and removes everything afterwards. The first run
takes a few minutes.

### Option 2: run the stack and call it yourself

```bash
# 1. Infrastructure for local development (Postgres on 5433, Redis on 6380, ElasticMQ on 9324)
docker compose up -d

# 2. Configure (see the variable list below), then load it
set -a; source .env; set +a

# 3. Database, dev certificates, seed data
bash scripts/run_migrations.sh
psql "$DATABASE_URL" -f sql/seed/seed.sql
bash scripts/gen_dev_certs.sh

# 4. Run the services, one per terminal (each after `set -a; source .env; set +a`)
(cd services/ledger && go run ./cmd/ledger)
(cd services/fx-service && npm install && npm run dev)
(cd services/api-gateway && npm install && npm run dev)
(cd services/settlement-worker && go run ./cmd/settlement-worker)
(cd services/reconciliation && go run ./cmd/reconciliation)

# 5. Create an API key (printed once) and call the API
KEY=$(bash scripts/create_api_key.sh)
curl -s -X POST localhost:3000/v1/transfers \
  -H "X-API-Key: $KEY" -H "Idempotency-Key: demo-1" -H 'content-type: application/json' \
  -d '{"from_wallet_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","to_wallet_id":"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb","amount":"10","currency":"USD"}'
```

The seed data contains Alice's USD wallet (`aaaaaaaa-...`, balance 1000) and Bob's NGN wallet (`bbbbbbbb-...`), plus
the system FX wallets. A USD to NGN transfer of 10 returns `converted_amount` 15000 at the dev rate of 1500.

Local `.env` (replace `/path/to/remit-platform` with your checkout; these are dev-only dummy values):

```bash
DATABASE_USER=remit
DATABASE_PASSWORD=remit
DATABASE_NAME=remit
DATABASE_URL=postgres://remit:remit@127.0.0.1:5433/remit?sslmode=disable
REDIS_URL=redis://127.0.0.1:6380
LEDGER_GRPC_ADDR=localhost:50051
FX_SERVICE_URL=http://localhost:3001
FX_REFRESH_MINUTES=5
FX_MAX_AGE=10m
SETTLEMENT_INTERVAL=1m
RECON_INTERVAL=24h
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=test
AWS_SECRET_ACCESS_KEY=test
SQS_ENDPOINT=http://localhost:9324
SQS_QUEUE_NAME=remit-events
SQS_CREATE_QUEUE=true
LEDGER_TLS_CERT=/path/to/remit-platform/certs/ledger.crt
LEDGER_TLS_KEY=/path/to/remit-platform/certs/ledger.key
LEDGER_TLS_CA=/path/to/remit-platform/certs/ca.crt
GATEWAY_TLS_CERT=/path/to/remit-platform/certs/gateway.crt
GATEWAY_TLS_KEY=/path/to/remit-platform/certs/gateway.key
GATEWAY_TLS_CA=/path/to/remit-platform/certs/ca.crt
```

### API

All routes except `/health` and `/metrics` need an `X-API-Key` header.

| Method and path | Purpose | Notable responses |
|---|---|---|
| `POST /v1/wallets` | Create a wallet `{owner_id, currency_code}` | 201, 409 if the owner already has one in that currency |
| `GET /v1/wallets/:id` | Read a wallet and its balance | 404 |
| `POST /v1/transfers` | Transfer `{from_wallet_id, to_wallet_id, amount, currency}`. **Requires `Idempotency-Key`.** | 201, 400 invalid, 404 unknown wallet, 409 key reused with a different payload, 422 insufficient funds or rate unavailable, 503 FX unavailable |
| `GET /v1/transactions/:id` | Read a transaction, including the FX rate used | 404 |
| `GET /v1/fx?from=USD&to=NGN` | Current rate | 404 unknown pair |
| `GET /health`, `GET /metrics` | Liveness and Prometheus metrics | no key needed |

Amounts are decimal **strings** (up to 14 integer and 6 decimal digits), never floats.

### Operating it

```bash
# Verify ledger integrity (exit code 1 on any mismatch)
(cd services/reconciliation && go run ./cmd/reconciliation -once)

# Audit trail: every ledger entry in a date range, with the FX rate used
bash scripts/audit_export.sh --from 2026-10-01 --to 2026-10-31 --format csv --out audit.csv

# Create an API key
bash scripts/create_api_key.sh
```

---

## Technical choices and trade-offs

Each choice below says what I picked, why, and what it costs.

### Correctness and data

- **PostgreSQL as the system of record, with constraints doing real work.**
  *Why:* ACID transactions, row locks and `CHECK` constraints give a safety net below the application code.
  *Cost:* the services share one database, which couples them. I limit the damage with a single-writer rule
  (only the ledger changes balances and entries) and by keeping the gateway's access read-only.
- **A `balance` column plus an append-only entries table.**
  *Why:* the overdraft check needs the balance under a row lock, and reading a balance should not mean summing history.
  *Cost:* two representations of the same fact. That is exactly what the reconciliation job exists to check.
- **Pessimistic locking, always in id order.**
  *Why:* a transfer locks both wallets with `SELECT ... FOR UPDATE` ordered by id, which serializes concurrent debits
  and rules out deadlocks between opposite-direction transfers. The tests run 50 parallel debits against a wallet
  and 60 opposite-direction transfers to check both.
  *Cost:* hot wallets are contention points (see the system FX wallets below).
- **Idempotency in the database.**
  *Why:* the key row is written in the same transaction as the transfer, and a per-key advisory lock makes parallel
  duplicates queue and then replay. A request hash catches the same key with a different payload.
  *Cost:* a rejected attempt (for example insufficient funds) does not record its key, so a retry is evaluated again.
  That is deliberate, since the balance may have changed.
- **Double-entry with per-currency system FX wallets.**
  *Why:* a cross-currency transfer posts four lines (debit sender, credit the system wallet in the sender's currency,
  debit the system wallet in the receiver's currency, credit receiver), so entries sum to zero in every currency.
  *Cost:* all cross-currency transfers lock the same two system rows, so they run one at a time. Sharding those wallets
  would fix it, at the price of a more complex ledger. This is on the improvements list.
- **Money as decimal strings and `NUMERIC(20,6)`, banker's rounding.**
  *Why:* floats lose cents. Converted amounts are rounded half-to-even to 2 decimals (tests pin `0.125` to `0.12` and
  `0.175` to `0.18`). The rate, source and timestamp are stored on the transaction, so history never depends on today's rates.
  *Cost:* the rounding rule is a fixed policy. A real product would make it per currency.

### Service boundaries and messaging

- **Go for the ledger and workers, Node.js and TypeScript for the edge.**
  *Why:* Go gives strict typing, simple concurrency and a good decimal library for the money path. Node and Fastify
  suit the I/O-heavy edge, with schema validation and fast iteration.
  *Cost:* two toolchains to build, test and containerize.
- **gRPC with mutual TLS between the gateway and the ledger.**
  *Why:* a typed contract shared from one proto file, and the ledger accepts only clients holding a certificate from
  the project CA.
  *Cost:* certificate management. Dev certificates come from a script. Production would use a proper CA or service mesh.
- **Transactional outbox, then SQS.**
  *Why:* writing the event in the same transaction as the transfer avoids the dual-write problem (the transfer
  commits but the publish fails, or the reverse). A relay publishes with `FOR UPDATE SKIP LOCKED`, so several ledger
  instances never send the same row twice at once.
  *Cost:* delivery is at-least-once, so consumers must be idempotent, and there is up to a couple of seconds of
  publish delay. Change-data-capture would be the heavier alternative.
- **Settlement reads the database, not the queue.**
  *Why:* the database is the source of truth, so a lost or duplicated message cannot lose a settlement. The queue is a notification channel.
  *Cost:* the worker polls. Netting is a single SQL statement that locks rows with `SKIP LOCKED`, so two workers never settle the same transaction.
- **A simulated rail behind an interface.**
  *Why:* it exercises the full settlement flow, including failure and retry, without a payment partner.
  *Cost:* real rails need attempt tracking, callbacks and partial-failure handling that are not built.

### Security and operations

- **API keys: hashed, looked up in Postgres, cached briefly.** Only SHA-256 hashes are stored. *Cost:* the gateway needs
  read access to the database; the alternative is an RPC to the ledger.
- **Rate limiting in Redis, 100 requests per minute per key.** *Why:* the limit holds across gateway instances.
  *Cost:* it fails open if Redis is down, favouring availability. Invalid-key attempts are not rate limited yet.
- **Structured logs with correlation ids.** The id enters at the gateway (or comes from the caller), travels in gRPC
  metadata, is logged by the ledger and stored on the transaction.
- **Reconciliation as a first-class feature,** not an afterthought: three invariant checks that write to a table and
  can fail a CI job or an alert by exit code.
- **Infrastructure: one EC2 host with Docker Compose, not Kubernetes.** *Why:* the aim is to show a clean, secure,
  cheap deployment without operating a cluster. *Cost:* a single host is a single point of failure, with no
  autoscaling. The Terraform removes the usual shortcuts (no SSH, private database, least-privilege IAM, IMDSv2).
- **Testing against real Postgres, not mocks.** Each test gets its own schema with the real migrations applied, so
  constraints and locking behave as in production.

### Decisions I changed along the way

- **EKS to EC2.** The first plan used Kubernetes. A single host is easier to explain, cheaper, and matches the goal of the project.
- **LocalStack to ElasticMQ.** LocalStack's current image needs a licence token even for SQS, so local development uses ElasticMQ,
  which speaks the same SQS API.
- **The first load test overturned an assumption.** I had expected same-currency transfers to scale and only
  cross-currency to be limited. The data said otherwise, so the write-up was corrected (below).

---

## Testing

| Layer | What it covers | How to run |
|---|---|---|
| Unit and database tests (Go) | Transfers, idempotency, FX rounding, insufficient funds, error codes, wallet queries, FX client failures, netting, settlement retries after rail failure, reconciliation mismatch detection | `export TEST_DATABASE_URL=postgres://...` then `go test ./... -cover` in each Go service |
| Concurrency tests | 50 parallel debits never overdraw (exactly 33 of 50 succeed on a balance of 100 with 3 each); 60 opposite-direction transfers do not deadlock; 20 parallel requests with one key create one transfer | part of the ledger tests |
| End-to-end test | The whole stack on a throwaway database, through the public API | `bash scripts/e2e.sh` |
| Load test | k6 against the gateway, then reconciliation | `bash scripts/load.sh` |

I held the ledger's transfer code to a target of 80% statement coverage (check with `go test ./... -cover`). CI runs the Go tests against a Postgres service, the Node
type-checks and the end-to-end test on every push.

### Performance

I ran the load test once, at 100 requests/second per scenario. **It failed its targets:** the system sustained about 7
requests/second, and p95 latency reached tens of seconds. Nothing was lost, though: there were no errors, and
reconciliation found no mismatches afterwards. All 362 cross-currency transfers were settled, and the database matched
the request counts exactly.

I have not found the bottleneck yet. Same-currency transfers were also slow, so the system-wallet locking is not the
only limit. The candidates (CPU contention on one host, the ledger's connection pool size, database waits) and the
steps to measure each are listed in [`docs/load-test.md`](docs/load-test.md), together with a flaw in the first run
(the two scenarios overlapped). I would rather publish a failed benchmark with an honest analysis than a number I
cannot defend.

---

## Security notes

- Gateway to ledger traffic uses mutual TLS; the ledger refuses to start without certificates.
- API keys are stored only as hashes; the database holds no usable credential.
- The AWS design has no SSH, a private encrypted database, least-privilege IAM, IMDSv2 and a generated database password.
- **Not done:** the public endpoint is plain HTTP on port 3000, with no TLS termination, WAF or per-key scopes.
  The dev certificates and dummy keys in the repo are for local use only.

---

## What I would add with more time

**Performance and scale**

- Find and fix the throughput limit. Size the ledger's connection pool, profile the transfer path, and re-run the load test at stepped rates with the scenarios isolated.
- Shard the system FX wallets (or batch the FX leg per corridor) so cross-currency transfers can run in parallel.
- Make the outbox relay drain until empty and use larger batches. Today it publishes up to 10 events every 2 seconds, which is about 5 events/second.
- Add secondary indexes (for example on `ledger_entries(wallet_id)` and the settlement query) and review the plans. Partition `ledger_entries` and use a read replica for exports.

**Deployment and operations**

- Apply and validate the Terraform, then add remote state with locking, separate environments, Multi-AZ RDS and an Elastic IP.
- CI/CD deploy: GitHub Actions to ECR and SSM using OIDC (no stored AWS keys), with the database password read from Secrets Manager at startup.
- TLS termination with an ALB and ACM certificate, plus a WAF.
- Prometheus and Grafana, OpenTelemetry tracing (the correlation id is not yet passed on to the FX service), and alerts on reconciliation mismatches and dead-letter queue depth.
- A restore drill for RDS backups, and a documented runbook for incidents such as a double debit.

**Product and domain**

- A real FX provider, rate quotes with an expiry, and fees and spread.
- Reversals and refunds as compensating entries, failed-transfer states and wallet freezes.
- A real settlement rail with attempt tracking, retries and callbacks.
- Consumers for the outbox events (webhooks, notifications), written to be idempotent.
- Compliance hooks: KYC and sanctions screening, and a tamper-evident audit log (for example a hash chain).
- Listing endpoints with pagination, an OpenAPI spec and generated client SDKs.

**Security**

- API key scopes, rotation, expiry and revocation, and rate limiting of failed authentication by IP.
- A managed certificate authority or service mesh in place of the dev certificate script.

**Testing**

- Property-based tests for the ledger invariants, and chaos tests that kill the ledger mid-transfer.
- Contract tests between the gateway and the ledger, and a load-test baseline tracked in CI.

---

## Repository layout

```text
remit-platform/
├── services/
│   ├── api-gateway/        Node.js, TypeScript, Fastify: REST, auth, rate limiting, metrics
│   ├── ledger/             Go: gRPC API, transfers, outbox relay (proto in proto/ledger.proto)
│   ├── fx-service/         Node.js, TypeScript: rates, Redis cache
│   ├── settlement-worker/  Go: netting and settlement
│   └── reconciliation/     Go: ledger integrity checks
├── sql/
│   ├── migrations/         Schema
│   └── seed/               Dev data and load-test data
├── infra/terraform/        VPC, EC2, RDS, SQS, ECR, IAM
├── scripts/                Migrations, certs, API keys, audit export, end-to-end and load tests
├── docs/load-test.md       Load test method, results and analysis
├── docker-compose.yml      Local development infrastructure
├── docker-compose.prod.yml The full stack from container images
├── docker-compose.e2e.yml  Throwaway infrastructure for the end-to-end and load tests
├── architecture.md         Design decisions and schema
└── tasks.md                The build plan this project followed
```
