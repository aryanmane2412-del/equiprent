ALTER TABLE bookings
  DROP CONSTRAINT IF EXISTS bookings_final_total_ck,
  DROP CONSTRAINT IF EXISTS bookings_final_ck,
  DROP COLUMN IF EXISTS finalized_at,
  DROP COLUMN IF EXISTS final_charges,
  DROP COLUMN IF EXISTS pickup,
  DROP COLUMN IF EXISTS delivery;

DROP TABLE IF EXISTS tax_rates;

ALTER TABLE equipment
  DROP CONSTRAINT IF EXISTS equipment_rental_window_ck,
  DROP COLUMN IF EXISTS cancellation_policy_id,
  DROP COLUMN IF EXISTS timezone,
  DROP COLUMN IF EXISTS late_grace_minutes,
  DROP COLUMN IF EXISTS late_fee_per_hour_paise,
  DROP COLUMN IF EXISTS fuel_rate_per_hour_paise,
  DROP COLUMN IF EXISTS security_deposit_paise,
  DROP COLUMN IF EXISTS max_rental_days,
  DROP COLUMN IF EXISTS min_rental_hours;

DELETE FROM cancellation_policies c WHERE c.owner_org_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM bookings b WHERE b.cancellation_policy_id = c.id);
DELETE FROM cancellation_policies c WHERE c.code = 'flexible'
  AND NOT EXISTS (SELECT 1 FROM bookings b WHERE b.cancellation_policy_id = c.id);
ALTER TABLE cancellation_policies
  DROP CONSTRAINT IF EXISTS cancellation_policies_code_check,
  ADD CONSTRAINT cancellation_policies_code_check CHECK (code ~ '^[a-z_]+$'),
  DROP COLUMN IF EXISTS owner_org_id;

ALTER TABLE mobilisation_rates
  DROP COLUMN IF EXISTS pickup_per_km_paise,
  DROP COLUMN IF EXISTS pickup_fixed_paise;
