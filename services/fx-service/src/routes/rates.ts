import { FastifyInstance } from "fastify";
import { RateService } from "../rates";

const querystring = {
  type: "object",
  required: ["from", "to"],
  properties: {
    from: { type: "string", pattern: "^[A-Z]{3}$" },
    to: { type: "string", pattern: "^[A-Z]{3}$" },
  },
} as const;

export function rateRoutes(rates: RateService) {
  return async function (app: FastifyInstance) {
    app.get<{ Querystring: { from: string; to: string } }>(
      "/rates",
      { schema: { querystring } },
      async (req, reply) => {
        const record = await rates.get(req.query.from, req.query.to);
        if (!record) return reply.status(404).send({ error: "rate not found" });
        return record;
      },
    );
  };
}