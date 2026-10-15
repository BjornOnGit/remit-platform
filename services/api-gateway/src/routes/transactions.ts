import { FastifyInstance } from "fastify";
import { getTransaction } from "../grpc/ledgerClient";

const idParams = {
  type: "object",
  required: ["id"],
  properties: { id: { type: "string", format: "uuid" } },
} as const;

export async function transactionRoutes(app: FastifyInstance) {
  app.get<{ Params: { id: string } }>(
    "/transactions/:id",
    { schema: { params: idParams } },
    async (req) => getTransaction(req.params.id, req.id),
  );
}