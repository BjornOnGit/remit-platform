import client from "prom-client";

client.collectDefaultMetrics();

export const register = client.register;

export const transferRequests = new client.Counter({
  name: "transfer_requests_total",
  help: "Total POST /v1/transfers requests handled by the gateway",
});

export const transferFailed = new client.Counter({
  name: "transfer_failed_total",
  help: "POST /v1/transfers requests that ended with an HTTP error status",
  labelNames: ["status"],
});