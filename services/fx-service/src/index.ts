import { Pool } from "pg";
import { buildApp } from "./app";
import { loadRates } from "./fetcher";
import { staticProvider } from "./providers/static";

async function main() {
  const dbUrl = process.env.DATABASE_URL;
  if (!dbUrl) throw new Error("DATABASE_URL is required");

  const pool = new Pool({ connectionString: dbUrl, max: 5 });
  const app = buildApp();

  const count = await loadRates(pool, staticProvider());
  app.log.info(`loaded ${count} fx rates`);

  await app.listen({ port: Number(process.env.PORT ?? 3001), host: "0.0.0.0" });
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});