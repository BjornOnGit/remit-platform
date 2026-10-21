import { createHash } from "node:crypto";
import { FastifyInstance } from "fastify";
import { Pool } from "pg";

// Resolves whether a key (given as its SHA-256 hex digest) exists.
export type KeyChecker = (keyHash: string) => Promise<boolean>;

export const dbKeyChecker =
  (pool: Pool): KeyChecker =>
  async (keyHash) => {
    const { rowCount } = await pool.query("SELECT 1 FROM api_keys WHERE key_hash = $1", [keyHash]);
    return (rowCount ?? 0) > 0;
  };

const PUBLIC_PATHS = new Set(["/health", "/metrics"]);
const VALID_TTL_MS = 60_000;
const INVALID_TTL_MS = 10_000; // short, so a newly created key works quickly
const MAX_CACHE_ENTRIES = 10_000;

// Requires a valid X-API-Key on every route except /health and /metrics.
// Only hashes are looked up or cached; the raw key is never stored.
export function registerApiKeyAuth(app: FastifyInstance, check: KeyChecker) {
  const cache = new Map<string, { ok: boolean; expires: number }>();

  app.addHook("onRequest", async (req, reply) => {
    if (PUBLIC_PATHS.has(req.url.split("?")[0])) return;

    const key = req.headers["x-api-key"];
    if (typeof key !== "string" || key === "") {
      return reply.code(401).send({ error: "missing API key" });
    }

    const hash = createHash("sha256").update(key).digest("hex");
    const now = Date.now();
    let entry = cache.get(hash);
    if (!entry || entry.expires < now) {
      let ok: boolean;
      try {
        ok = await check(hash);
      } catch (err) {
        req.log.error(err, "api key lookup failed");
        return reply.code(503).send({ error: "authentication unavailable" });
      }
      entry = { ok, expires: now + (ok ? VALID_TTL_MS : INVALID_TTL_MS) };
      if (cache.size >= MAX_CACHE_ENTRIES) cache.clear();
      cache.set(hash, entry);
    }

    if (!entry.ok) return reply.code(401).send({ error: "invalid API key" });
  });
}