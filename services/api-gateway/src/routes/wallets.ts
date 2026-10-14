import { FastifyInstance } from "fastify";
import { createWallet, getWallet } from "../grpc/ledgerClient";

const createBody = {
  type: "object",
  required: ["owner_id", "currency_code"],
  additionalProperties: false,
  properties: {
    owner_id: { type: "string", format: "uuid" },
    currency_code: { type: "string", pattern: "^[A-Z]{3}$" },
  },
} as const;

const idParams = {
  type: "object",
  required: ["id"],
  properties: { id: { type: "string", format: "uuid" } },
} as const;

export async function walletRoutes(app: FastifyInstance) {
  app.post<{ Body: { owner_id: string; currency_code: string } }>(
    "/wallets",
    { schema: { body: createBody } },
    async (req, reply) => {
      const wallet = await createWallet(req.body.owner_id, req.body.currency_code);
      return reply.status(201).send(wallet);
    },
  );

  app.get<{ Params: { id: string } }>(
    "/wallets/:id",
    { schema: { params: idParams } },
    async (req) => getWallet(req.params.id),
  );
}