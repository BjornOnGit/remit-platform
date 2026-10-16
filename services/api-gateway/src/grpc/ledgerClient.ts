import fs from "node:fs";
import path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";

// The proto file in services/ledger/proto is the single source of truth.
const PROTO_PATH =
  process.env.LEDGER_PROTO_PATH ??
  path.resolve(__dirname, "../../../ledger/proto/ledger.proto");

const LEDGER_ADDR = process.env.LEDGER_GRPC_ADDR ?? "localhost:50051";

const definition = protoLoader.loadSync(PROTO_PATH, {
  keepCase: true, // keep snake_case field names from the proto
  longs: String,
  enums: String,
  defaults: true,
  oneofs: true,
});

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const proto = grpc.loadPackageDefinition(definition) as any;

function readEnvFile(name: string): Buffer {
  const file = process.env[name];
  if (!file) throw new Error(`${name} is required (run scripts/gen_dev_certs.sh)`);
  return fs.readFileSync(file);
}

// mTLS: verify the ledger's certificate and present our own client certificate.
const credentials = grpc.credentials.createSsl(
  readEnvFile("GATEWAY_TLS_CA"),
  readEnvFile("GATEWAY_TLS_KEY"),
  readEnvFile("GATEWAY_TLS_CERT"),
);

const Ledger = proto.ledger?.v1?.Ledger;
if (!Ledger) {
  throw new Error(
    `ledger.v1.Ledger service not found in ${PROTO_PATH}; definitions loaded: ${
      Object.keys(definition).join(", ") || "(none)"
    }`,
  );
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const client: any = new Ledger(LEDGER_ADDR, credentials);

// The correlation id travels to the ledger as gRPC metadata.
function unary<Req, Res>(method: string, req: Req, correlationId?: string): Promise<Res> {
  const metadata = new grpc.Metadata();
  if (correlationId) metadata.set("x-correlation-id", correlationId);
  return new Promise((resolve, reject) => {
    client[method](req, metadata, (err: grpc.ServiceError | null, res: Res) =>
      err ? reject(err) : resolve(res),
    );
  });
}

export interface Wallet {
  id: string;
  owner_id: string;
  currency_code: string;
  balance: string;
  created_at: string;
}

export const getWallet = (id: string, cid?: string) =>
  unary<{ id: string }, Wallet>("GetWallet", { id }, cid);

export const createWallet = (owner_id: string, currency_code: string, cid?: string) =>
  unary<{ owner_id: string; currency_code: string }, Wallet>(
    "CreateWallet",
    { owner_id, currency_code },
    cid,
  );

export interface Transaction {
  id: string;
  from_wallet_id: string;
  to_wallet_id: string;
  amount: string;
  currency_from: string;
  currency_to: string;
  converted_amount: string;
  fx_rate: string;
  fx_source: string;
  fx_timestamp: string;
  status: string;
  created_at: string;
}

export interface TransferInput {
  idempotency_key: string;
  from_wallet_id: string;
  to_wallet_id: string;
  amount: string;
  currency: string;
  metadata?: Record<string, string>;
}

export const transfer = async (input: TransferInput, cid?: string): Promise<Transaction> => {
  const res = await unary<TransferInput, { transaction: Transaction }>("Transfer", input, cid);
  return res.transaction;
};

export const getTransaction = (id: string, cid?: string) =>
  unary<{ id: string }, Transaction>("GetTransaction", { id }, cid);