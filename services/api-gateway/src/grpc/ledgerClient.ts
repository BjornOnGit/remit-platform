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

// TODO(Task 6.3): switch to mTLS credentials.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const client: any = new proto.ledger.v1.Ledger(
  LEDGER_ADDR,
  grpc.credentials.createInsecure(),
);

function unary<Req, Res>(method: string, req: Req): Promise<Res> {
  return new Promise((resolve, reject) => {
    client[method](req, (err: grpc.ServiceError | null, res: Res) =>
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

export const getWallet = (id: string) =>
  unary<{ id: string }, Wallet>("GetWallet", { id });

export const createWallet = (owner_id: string, currency_code: string) =>
  unary<{ owner_id: string; currency_code: string }, Wallet>("CreateWallet", {
    owner_id,
    currency_code,
  });

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

export const transfer = async (input: TransferInput): Promise<Transaction> => {
  const res = await unary<TransferInput, { transaction: Transaction }>("Transfer", input);
  return res.transaction;
};