import { buildApp } from "./app";
import { dbKeyChecker } from "./auth";
import { createPool } from "./db";

const dbUrl = process.env.DATABASE_URL;
if (!dbUrl) {
  console.error("DATABASE_URL is required (used to validate API keys)");
  process.exit(1);
}

const port = Number(process.env.PORT ?? 3000);

buildApp(dbKeyChecker(createPool(dbUrl)))
  .listen({ port, host: "0.0.0.0" })
  .catch((err) => {
    console.error(err);
    process.exit(1);
  });