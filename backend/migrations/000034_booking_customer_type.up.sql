-- Customer rental type chosen at booking time, with the contact/business details the customer entered.
-- Existing bookings default to 'individual' with empty details (owner view falls back to the account contact).
ALTER TABLE bookings
  ADD COLUMN customer_type text NOT NULL DEFAULT 'individual' CHECK (customer_type IN ('individual', 'company')),
  ADD COLUMN customer_details jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(customer_details) = 'object');
