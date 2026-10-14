import { Pool } from "pg";
import type { Redis } from "ioredis";

export interface RateRecord {
  from: string;
  to: string;
  rate: string;
  source: string;
  fetched_at: string;
}

export interface RateService {
  get(from: string, to: string): Promise<RateRecord | null>;
}

// Latest rate for a pair: Redis first, Postgres as fallback (and when Redis is down).
export function createRateService(pool: Pool, redis: Redis, ttlSeconds = 60): RateService {
  return {
    async get(from, to) {
      const key = `fx:rate:${from}:${to}`;

      try {
        const cached = await redis.get(key);
        if (cached) return JSON.parse(cached) as RateRecord;
      } catch {
        // cache unavailable: fall through to the database
      }

      const { rows } = await pool.query(
        `SELECT rate::text AS rate, source, fetched_at
         FROM fx_rates
         WHERE base_currency = $1 AND quote_currency = $2
         ORDER BY fetched_at DESC, id DESC
         LIMIT 1`,
        [from, to],
      );
      if (rows.length === 0) return null;

      const record: RateRecord = {
        from,
        to,
        rate: rows[0].rate,
        source: rows[0].source,
        fetched_at: new Date(rows[0].fetched_at).toISOString(),
      };

      try {
        await redis.set(key, JSON.stringify(record), "EX", ttlSeconds);
      } catch {
        // ignore cache write failures
      }
      return record;
    },
  };
}