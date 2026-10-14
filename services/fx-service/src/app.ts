import Fastify from "fastify";
import { RateService } from "./rates";
import { rateRoutes } from "./routes/rates";

export function buildApp(rates: RateService) {
  const app = Fastify({ logger: true });

  app.get("/health", async () => ({ status: "ok" }));
  app.register(rateRoutes(rates));

  return app;
}