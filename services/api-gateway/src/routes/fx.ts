import { FastifyInstance } from "fastify";
import { getRate } from "../services/fxClient";

const querystring = {
  type: "object",
  required: ["from", "to"],
  properties: {
    from: { type: "string", pattern: "^[A-Z]{3}$" },
    to: { type: "string", pattern: "^[A-Z]{3}$" },
  },
} as const;

export async function fxRoutes(app: FastifyInstance) {
  app.get<{ Querystring: { from: string; to: string } }>(
    "/fx",
    { schema: { querystring } },
    async (req) => getRate(req.query.from, req.query.to),
  );
}