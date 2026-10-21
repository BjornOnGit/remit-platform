import { createHash } from "node:crypto";
import rateLimit from "@fastify/rate-limit";
import { FastifyInstance } from "fastify";
import type { Redis } from "ioredis";

const PUBLIC_PATHS = new Set(["/health", "/metrics"]);

// Limits each API key to RATE_LIMIT_MAX requests per RATE_LIMIT_WINDOW_SECONDS (default 100 per
// minute), counted in Redis so the limit holds across gateway instances. Register after the
// auth hook, so only requests with a valid key are counted. If Redis is unavailable, requests
// are allowed (the limiter fails open) rather than taking the API down.
export function registerRateLimit(app: FastifyInstance, redis: Redis) {
  const max = Number(process.env.RATE_LIMIT_MAX ?? 100);
  const windowSeconds = Number(process.env.RATE_LIMIT_WINDOW_SECONDS ?? 60);
  if (!(max > 0) || !(windowSeconds > 0)) {
    throw new Error("RATE_LIMIT_MAX and RATE_LIMIT_WINDOW_SECONDS must be positive numbers");
  }

  app.register(rateLimit, {
    max,
    timeWindow: windowSeconds * 1000,
    redis,
    nameSpace: "remit:rl:",
    skipOnError: true,
    allowList: (req) => PUBLIC_PATHS.has(req.url.split("?")[0]),
    // Count per key, but store only a hash of it.
    keyGenerator: (req) => {
      const key = req.headers["x-api-key"];
      return typeof key === "string"
        ? `key:${createHash("sha256").update(key).digest("hex")}`
        : `ip:${req.ip}`;
    },
    errorResponseBuilder: (_req, ctx) =>
      Object.assign(new Error("rate limit exceeded"), {
        statusCode: 429,
        retryAfterSeconds: Math.ceil(ctx.ttl / 1000),
      }),
  });
}