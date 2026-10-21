# Load test

How the transfer path behaves under sustained load, what the first run showed, and what to do next.

## What is tested

`POST /v1/transfers` through the API gateway at a constant arrival rate (default 100 requests/second),
against the full stack running in Docker Compose on a throwaway Postgres. Two scenarios run one after the other:

| Scenario           | Traffic                                                                  | What it is meant to show                                            |
| ------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------- |
| `same_currency`  | USD to USD between 100 sender and 100 receiver wallets, chosen at random | Behaviour when transfers touch mostly independent rows.             |
| `cross_currency` | USD to NGN between the same senders and 100 NGN wallets                  | Behaviour when every transfer also locks the two system FX wallets. |

Each request uses a unique idempotency key and moves 0.01 USD. The settlement worker runs every 5 seconds during the
test, so netting and settlement load is part of the picture. Afterwards, reconciliation checks that every wallet
balance still equals the sum of its ledger entries and that every transaction balances to zero.

## How to run it

```bash
bash scripts/load.sh                          # 100 rps for 30s per scenario
RATE=200 DURATION=60s bash scripts/load.sh    # heavier
```

Needs Docker, `psql` and `openssl`. k6 runs from the `grafana/k6` image. The k6 summary is written to
`docs/load-test-results/summary.json`.

## Results (run of 2026-10-20)

Machine: _463.17 GiB / 7.66 GiB / Fedora Linux 44 (Workstation Edition) x86_64_ · Postgres 15 in Docker · one instance of every service · everything on one host,
including the load generator · target: 100 rps for 30s per scenario (6000 requests scheduled in total).

| Scenario       | Completed | Failed | avg    | p50    | p90    | p95    | max    |
| -------------- | --------- | ------ | ------ | ------ | ------ | ------ | ------ |
| same_currency  | 485       | 0      | 24.1 s | 30.0 s | 35.9 s | 36.9 s | 38.3 s |
| cross_currency | 216       | 0      | 36.2 s | 35.5 s | 51.9 s | 52.7 s | 56.3 s |
| all requests   | 701       | 0      | 27.9 s | 30.9 s | 38.3 s | 50.5 s | 56.3 s |

- **Thresholds:** failed. Targets were p95 < 500 ms (same-currency) and < 1 s (cross-currency); measured 36.9 s and 52.7 s.
- **Throughput:** about 7.4 completed requests/s over the 95-second run, against a target of 100/s.
- **Dropped iterations:** 5155 of the ~6000 scheduled. k6 could not start them because all 300 virtual users
  per scenario were already waiting on slow responses. A further 146 requests were still in flight when the run ended.
- **Errors:** none. All 701 completed requests returned 201, and the checks passed 100%.
- **Fastest request:** 309 ms. Under light load the path works; it degrades as load builds.

### Correctness under overload

Reconciliation after the run reported no mismatches. The database agrees with k6:

- 847 transfers were committed: the 701 completed plus the 146 interrupted ones, which still went through.
- 485 same-currency plus 362 cross-currency transfers. All 362 cross-currency transfers were settled by the worker
  (`settled=362`), so settlement kept up with the backlog.
- `posted=586` is not a stuck backlog. It is the 485 same-currency transfers, which are never settled, plus the
  101 funding transactions created by the seed scripts.

## What the numbers say, and what they don't

**What they show.** The system sustained roughly 7 requests/s, far below the 100/s offered. Latency was almost entirely
waiting: the request-level metrics put time in `http_req_waiting` (avg 27.9 s) and almost none in connecting, sending or
receiving. That is the signature of an overloaded system with a growing queue, not of one slow query. The service stayed
correct throughout: no errors, no lost or duplicated money.

**What they don't show.**

- **The cross-currency numbers are not clean.** The second scenario started 35 s into the run, while the first was still
  working through its backlog (its slowest request took 38 s). The cross-currency minimum of 20 s shows it inherited that
  queue. Comparing the two scenarios from this run is therefore not valid.
- **The bottleneck is not identified yet.** Same-currency transfers were also very slow, even though they share no
  wallet rows beyond the random pool of 100. So the system-wallet locking cannot be the
  only limit, and may not be the main one. Candidates that fit the data, each still to be measured:
  - CPU contention: the load generator (up to 600 virtual users), five services, Postgres, Redis and ElasticMQ all share one host.
  - The ledger's database connection pool is small by default (pgx sizes it from the CPU count), and each transfer
    holds a connection across about a dozen statements.
  - Lock queues or I/O waits inside Postgres, from each commit and the outbox and settlement workers running alongside.
- **No server-side timings were collected,** so there is no way yet to say whether time was spent in the gateway, in
  gRPC queuing, or in the database.

## Next steps to find the limit

1. **Find the knee.** Run at a low rate and step up: `RATE=10`, `25`, `50`, `100`. Note the rate at which p95 first
   exceeds 500 ms. That, not the 100 rps target, is the system's current capacity.
2. **Isolate the scenarios** so one cannot pollute the other: run same-currency alone, then cross-currency alone,
   or leave a long gap between them.
3. **Look inside during a run:**
   - `docker stats` to see whether the host or a single container is CPU-bound.
   - `select wait_event_type, wait_event, count(*) from pg_stat_activity group by 1, 2;` repeatedly, to see whether
     queries are waiting on locks, I/O or clients.
   - Compare the ledger's own `duration_ms` in its request logs with the latency k6 sees. Small ledger durations
     with large client latency point to queueing before the ledger; large ledger durations point at the database.
4. **Change one thing at a time** (for example the ledger's pool size via `pool_max_conns` in `DATABASE_URL`) and
   re-run, recording each result here.

## Limitations

- Everything runs on one machine, so the load generator, services and database compete for the same CPU.
- The database is a small Postgres container with default settings, not RDS. Absolute numbers on AWS will differ.
- Wallet ids are random within a fixed pool of 100, so some contention on sender wallets is expected.
- The first run overloaded the system by design of the target, so it measures failure behaviour, not capacity.
