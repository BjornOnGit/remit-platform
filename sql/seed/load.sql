-- Load-test data (dev only). Run after sql/seed/seed.sql; safe to run more than once.
-- Creates 100 funded USD sender wallets (10000 each), 100 USD receivers and 100 NGN receivers
-- with deterministic ids, funded through real ledger transactions so reconciliation stays clean:
--   senders    10000000-0000-0000-0000-0000000000NN
--   receivers  20000000-0000-0000-0000-0000000000NN (USD)
--   ngn        40000000-0000-0000-0000-0000000000NN (NGN)
DO $$
DECLARE
  i int;
  sys_usd uuid;
  sender uuid;
  receiver uuid;
  ngn uuid;
  fund uuid;
BEGIN
  SELECT id INTO sys_usd FROM wallets WHERE is_system AND currency_code = 'USD';
  IF sys_usd IS NULL THEN
    RAISE EXCEPTION 'run sql/seed/seed.sql first';
  END IF;
  IF EXISTS (SELECT 1 FROM wallets WHERE id = '10000000-0000-0000-0000-000000000001') THEN
    RETURN;
  END IF;

  FOR i IN 1..100 LOOP
    sender   := ('10000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid;
    receiver := ('20000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid;
    ngn      := ('40000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid;

    INSERT INTO wallets (id, owner_id, currency_code, balance) VALUES
      (sender,   gen_random_uuid(), 'USD', 10000),
      (receiver, gen_random_uuid(), 'USD', 0),
      (ngn,      gen_random_uuid(), 'NGN', 0);

    INSERT INTO transactions (from_wallet_id, to_wallet_id, amount, currency_from, currency_to, converted_amount)
    VALUES (sys_usd, sender, 10000, 'USD', 'USD', 10000)
    RETURNING id INTO fund;

    INSERT INTO ledger_entries (wallet_id, transaction_id, amount, currency_code, entry_type) VALUES
      (sys_usd, fund, -10000, 'USD', 'seed_funding'),
      (sender,  fund,  10000, 'USD', 'seed_funding');
  END LOOP;

  UPDATE wallets SET balance = balance - 1000000 WHERE id = sys_usd;
END $$;