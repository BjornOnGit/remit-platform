import { FastifyInstance } from "fastify";
import { transfer } from "../grpc/ledgerClient";

const body = {
  type: "object",
  required: ["from_wallet_id", "to_wallet_id", "amount", "currency"],
  additionalProperties: false,
  properties: {
    from_wallet_id: { type: "string", format: "uuid" },
    to_wallet_id: { type: "string", format: "uuid" },
    // decimal string, up to 14 integer digits and 6 decimals (matches numeric(20,6))
    amount: { type: "string", pattern: "^\\d{1,14}(\\.\\d{1,6})?$" },
    currency: { type: "string", pattern: "^[A-Z]{3}$" },
    metadata: { type: "object", additionalProperties: { type: "string" } },
  },
} as const;

const headers = {
  type: "object",
  required: ["idempotency-key"],
  properties: {
    "idempotency-key": { type: "string", minLength: 1, maxLength: 128 },
  },
} as const;

interface TransferBody {
  from_wallet_id: string;
  to_wallet_id: string;
  amount: string;
  currency: string;
  metadata?: Record<string, string>;
}

export async function transferRoutes(app: FastifyInstance) {
  app.post<{ Body: TransferBody; Headers: { "idempotency-key": string } }>(
    "/transfers",
    { schema: { body, headers } },
    async (req, reply) => {
      const tx = await transfer({
        ...req.body,
        idempotency_key: req.headers["idempotency-key"],
      });
      return reply.status(201).send(tx);
    },
  );
}