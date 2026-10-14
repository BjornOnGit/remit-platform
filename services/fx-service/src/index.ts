import { Pool } from "pg";
import Redis from "ioredis";
import { buildApp } from "./app";
import { loadRates } from "./fetcher";
import { staticProvider } from "./providers/static";
import { createRateService } from "./rates";

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

  const count = await loadRates(pool, staticProvider());
  app.log.info(`loaded ${count} fx rates`);

  await app.listen({ port: Number(process.env.PORT ?? 3001), host: "0.0.0.0" });
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});