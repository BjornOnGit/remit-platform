import Fastify from "fastify";
import client from "prom-client";
import { RateService } from "./rates";
import { rateRoutes } from "./routes/rates";

client.collectDefaultMetrics();

export function buildApp(rates: RateService) {
  const app = Fastify({ logger: true });

  app.get("/health", async () => ({ status: "ok" }));
  app.get("/metrics", async (_req, reply) =>
    reply.header("content-type", client.register.contentType).send(await client.register.metrics()),
  );
  app.register(rateRoutes(rates));

  return app;
}