import { Pool } from "pg";
import Redis from "ioredis";
import { buildApp } from "./app";
import { loadRates } from "./fetcher";
import { staticProvider } from "./providers/static";
import { createRateService } from "./rates";
import { startRefresh } from "./refresh";

async function main() {
  const dbUrl = process.env.DATABASE_URL;
  if (!dbUrl) throw new Error("DATABASE_URL is required");

  const pool = new Pool({ connectionString: dbUrl, max: 5 });
  const redis = new Redis(process.env.REDIS_URL ?? "redis://127.0.0.1:6379", {
    enableOfflineQueue: false,
    maxRetriesPerRequest: 1,
  });
  redis.on("error", () => {
    // cache is optional; lookups fall back to Postgres
  });

  const app = buildApp(createRateService(pool, redis));

  const refreshMinutes = Number(process.env.FX_REFRESH_MINUTES ?? 5);
  if (!(refreshMinutes > 0)) throw new Error("FX_REFRESH_MINUTES must be a positive number");

  const provider = staticProvider();
  const count = await loadRates(pool, provider);
  app.log.info(`loaded ${count} fx rates`);
  startRefresh(() => loadRates(pool, provider), refreshMinutes * 60_000, app.log);

  await app.listen({ port: Number(process.env.PORT ?? 3001), host: "0.0.0.0" });
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});