-- Dev seed. Safe to run more than once.
-- System FX wallets (one per currency) and two test user wallets.

INSERT INTO wallets (id, owner_id, currency_code, is_system) VALUES
  ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000001', 'USD', true),
  ('00000000-0000-0000-0000-0000000000a2', '00000000-0000-0000-0000-000000000001', 'NGN', true),
  ('00000000-0000-0000-0000-0000000000a3', '00000000-0000-0000-0000-000000000001', 'EUR', true),
  ('00000000-0000-0000-0000-0000000000a4', '00000000-0000-0000-0000-000000000001', 'GBP', true)
ON CONFLICT DO NOTHING;

INSERT INTO wallets (id, owner_id, currency_code) VALUES
  ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', '11111111-1111-1111-1111-111111111111', 'USD'),
  ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', '22222222-2222-2222-2222-222222222222', 'NGN')
ON CONFLICT DO NOTHING;

-- Fund Alice's USD wallet with 1000 via a real ledger transaction
-- (system USD wallet -> Alice), so balances stay equal to the sum of entries.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM transactions WHERE id = 'cccccccc-cccc-cccc-cccc-cccccccccccc') THEN
    INSERT INTO transactions (id, from_wallet_id, to_wallet_id, amount, currency_from, currency_to, converted_amount)
    VALUES ('cccccccc-cccc-cccc-cccc-cccccccccccc',
            '00000000-0000-0000-0000-0000000000a1',
            'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
            1000, 'USD', 'USD', 1000);

    INSERT INTO ledger_entries (wallet_id, transaction_id, amount, currency_code, entry_type) VALUES
      ('00000000-0000-0000-0000-0000000000a1', 'cccccccc-cccc-cccc-cccc-cccccccccccc', -1000, 'USD', 'seed_funding'),
      ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'cccccccc-cccc-cccc-cccc-cccccccccccc',  1000, 'USD', 'seed_funding');

    UPDATE wallets SET balance = balance - 1000 WHERE id = '00000000-0000-0000-0000-0000000000a1';
    UPDATE wallets SET balance = balance + 1000 WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa';
  END IF;
END $$;