import fs from "node:fs";
import path from "node:path";

export interface Rate {
  base: string;
  quote: string;
  rate: string; // decimal string
}

export interface RateProvider {
  name: string;
  fetch(): Promise<Rate[]>;
}

// Dev provider: reads USD-based rates from a JSON file and derives every currency pair.
export function staticProvider(
  file = process.env.FX_RATES_FILE ?? path.resolve(__dirname, "../../data/rates.json"),
): RateProvider {
  return {
    name: "static",
    async fetch() {
      const data = JSON.parse(fs.readFileSync(file, "utf8")) as {
        base: string;
        rates: Record<string, number>;
      };
      const perBase: Record<string, number> = { [data.base]: 1, ...data.rates };
      const out: Rate[] = [];
      for (const from of Object.keys(perBase)) {
        for (const to of Object.keys(perBase)) {
          if (from === to) continue;
          out.push({ base: from, quote: to, rate: (perBase[to] / perBase[from]).toFixed(12) });
        }
      }
      return out;
    },
  };
}