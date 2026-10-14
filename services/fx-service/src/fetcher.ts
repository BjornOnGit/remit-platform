import { Pool } from "pg";
import { RateProvider } from "./providers/static";

// Fetches rates from the provider and stores them as new rows in fx_rates.
export async function loadRates(pool: Pool, provider: RateProvider): Promise<number> {
  const rates = await provider.fetch();
  const client = await pool.connect();
  try {
    await client.query("BEGIN");
    for (const r of rates) {
      await client.query(
        `INSERT INTO fx_rates (base_currency, quote_currency, rate, source)
         VALUES ($1, $2, $3, $4)`,
        [r.base, r.quote, r.rate, provider.name],
      );
    }
    await client.query("COMMIT");
  } catch (err) {
    await client.query("ROLLBACK");
    throw err;
  } finally {
    client.release();
  }
  return rates.length;
}