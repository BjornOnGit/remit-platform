// k6 load test for POST /v1/transfers. Needs sql/seed/load.sql (100 funded senders).
//   RATE      requests per second per scenario (default 100)
//   DURATION  length of each scenario, in seconds like "30s" (default 30s)
//   BASE_URL  gateway URL (default http://localhost:3300)
// Scenario 1 sends same-currency transfers across many wallet pairs (can run in parallel).
// Scenario 2 sends USD->NGN transfers; every one locks the two system FX wallets, so they
// serialize on those rows. Running both shows the cost of that design choice.
import http from "k6/http";
import { check } from "k6";

const BASE = __ENV.BASE_URL || "http://localhost:3300";
const RATE = Number(__ENV.RATE || 100);
const DURATION = __ENV.DURATION || "30s";
const WALLETS = 100;
const secs = parseInt(DURATION, 10);

const arrival = (exec, startTime) => ({
  executor: "constant-arrival-rate",
  exec,
  startTime,
  rate: RATE,
  timeUnit: "1s",
  duration: DURATION,
  preAllocatedVUs: 50,
  maxVUs: 300,
});

export const options = {
  scenarios: {
    same_currency: arrival("sameCurrency", "0s"),
    cross_currency: arrival("crossCurrency", `${secs + 5}s`),
  },
  thresholds: {
    "http_req_failed{scenario:same_currency}": ["rate<0.01"],
    "http_req_duration{scenario:same_currency}": ["p(95)<500"],
    "http_req_failed{scenario:cross_currency}": ["rate<0.01"],
    "http_req_duration{scenario:cross_currency}": ["p(95)<1000"],
  },
};

const walletId = (prefix, i) => `${prefix}0000000-0000-0000-0000-${String(i).padStart(12, "0")}`;
const pick = () => Math.floor(Math.random() * WALLETS) + 1;

function transfer(toPrefix) {
  const body = JSON.stringify({
    from_wallet_id: walletId("1", pick()),
    to_wallet_id: walletId(toPrefix, pick()),
    amount: "0.01",
    currency: "USD",
  });
  const res = http.post(`${BASE}/v1/transfers`, body, {
    headers: {
      "content-type": "application/json",
      "X-API-Key": __ENV.API_KEY,
      "Idempotency-Key": `${__VU}-${__ITER}-${Date.now()}-${Math.random()}`,
    },
  });
  check(res, { "status is 201": (r) => r.status === 201 });
}

export function sameCurrency() {
  transfer("2");
}

export function crossCurrency() {
  transfer("4");
}