import Fastify from "fastify";
import { httpStatusFor, isGrpcError } from "./grpc/errors";
import { FxError } from "./services/fxClient";
import { fxRoutes } from "./routes/fx";
import { transactionRoutes } from "./routes/transactions";
import { transferRoutes } from "./routes/transfers";
import { walletRoutes } from "./routes/wallets";

export function buildApp() {
  const app = Fastify({ logger: true });

  app.setErrorHandler((err, _req, reply) => {
    if (isGrpcError(err)) {
      const code = httpStatusFor(err.code);
      if (code >= 500) app.log.error(err);
      const message =
        code === 503
          ? err.details === "fx service unavailable"
            ? err.details
            : "ledger unavailable"
          : code === 500
            ? "internal error"
            : err.details;
      return reply.status(code).send({ error: message });
    }
    if (err instanceof FxError) {
      return reply.status(err.statusCode).send({ error: err.message });
    }
    return reply.send(err);
  });

  app.get("/health", async () => ({ status: "ok" }));
  app.register(walletRoutes, { prefix: "/v1" });
  app.register(transferRoutes, { prefix: "/v1" });
  app.register(transactionRoutes, { prefix: "/v1" });
  app.register(fxRoutes, { prefix: "/v1" });

  return app;
}