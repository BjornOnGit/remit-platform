CREATE TABLE wallets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id uuid NOT NULL,
  currency_code char(3) NOT NULL,
  balance numeric(20,6) NOT NULL DEFAULT 0,
  is_system boolean NOT NULL DEFAULT false,
  metadata jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (owner_id, currency_code),
  CHECK (is_system OR balance >= 0)
);

CREATE TABLE settlements (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  corridor varchar(7) NOT NULL,
  currency_code char(3) NOT NULL,
  net_amount numeric(20,6) NOT NULL,
  status varchar(32) NOT NULL DEFAULT 'created' CHECK (status IN ('created','settled','failed')),
  created_at timestamptz NOT NULL DEFAULT now(),
  settled_at timestamptz
);

CREATE TABLE transactions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  from_wallet_id uuid NOT NULL REFERENCES wallets(id),
  to_wallet_id uuid NOT NULL REFERENCES wallets(id),
  amount numeric(20,6) NOT NULL CHECK (amount > 0),
  currency_from char(3) NOT NULL,
  currency_to char(3) NOT NULL,
  converted_amount numeric(20,6) NOT NULL,
  fx_rate numeric(30,12),
  fx_source varchar(64),
  fx_timestamp timestamptz,
  corridor varchar(7),
  status varchar(32) NOT NULL DEFAULT 'posted'
    CHECK (status IN ('posted','in_settlement','settled','failed')),
  settlement_id uuid REFERENCES settlements(id),
  correlation_id varchar(128),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ledger_entries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  wallet_id uuid NOT NULL REFERENCES wallets(id),
  transaction_id uuid NOT NULL REFERENCES transactions(id),
  amount numeric(20,6) NOT NULL CHECK (amount <> 0),
  currency_code char(3) NOT NULL,
  entry_type varchar(32) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE idempotency_keys (
  key varchar(128) PRIMARY KEY,
  request_hash char(64) NOT NULL,
  transaction_id uuid NOT NULL REFERENCES transactions(id),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE fx_rates (
  id bigserial PRIMARY KEY,
  base_currency char(3) NOT NULL,
  quote_currency char(3) NOT NULL,
  rate numeric(30,12) NOT NULL CHECK (rate > 0),
  source varchar(64) NOT NULL,
  fetched_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX fx_rates_pair_idx ON fx_rates (base_currency, quote_currency, fetched_at DESC);

CREATE TABLE outbox_events (
  id bigserial PRIMARY KEY,
  event_type varchar(64) NOT NULL,
  payload jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  published_at timestamptz
);

CREATE TABLE reconciliation_logs (
  id bigserial PRIMARY KEY,
  run_at timestamptz NOT NULL DEFAULT now(),
  check_name varchar(64) NOT NULL,
  expected numeric(20,6),
  actual numeric(20,6),
  status varchar(16) NOT NULL CHECK (status IN ('ok','mismatch')),
  details jsonb
);

CREATE TABLE api_keys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id uuid NOT NULL,
  key_hash char(64) NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);