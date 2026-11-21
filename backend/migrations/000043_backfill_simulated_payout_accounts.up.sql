-- Owners whose bank account was verified before payout accounts were created at verification
-- time have no payout account and would never be paid. Backfill them, for SIMULATED accounts
-- only (fa_sbx_ references, development/staging): in production a bank account could not be
-- verified before live payouts existed, so there is nothing to guess about live accounts.
INSERT INTO payout_linked_accounts (owner_org_id, provider, mode, account_id, activation_status, requirements, bank_account_id)
SELECT ba.organization_id, 'simulated', 'simulated', ba.gateway_linked_account_id, 'activated', '[]', ba.id
FROM bank_accounts ba
WHERE ba.is_primary AND ba.deleted_at IS NULL AND ba.verification_status = 'verified'
  AND ba.gateway_linked_account_id LIKE 'fa\_sbx\_%'
ON CONFLICT DO NOTHING;
