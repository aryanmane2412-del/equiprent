-- New booking statuses for the complete booking engine (used from 000019).
-- PostgreSQL cannot use an enum value in the transaction that adds it, so
-- the values are added on their own here.
ALTER TYPE booking_status ADD VALUE IF NOT EXISTS 'draft' BEFORE 'requested';
ALTER TYPE booking_status ADD VALUE IF NOT EXISTS 'payment_processing' AFTER 'payment_pending';
ALTER TYPE booking_status ADD VALUE IF NOT EXISTS 'extension_requested' AFTER 'active';
ALTER TYPE booking_status ADD VALUE IF NOT EXISTS 'cancellation_requested' AFTER 'disputed';
ALTER TYPE booking_status ADD VALUE IF NOT EXISTS 'refund_pending' AFTER 'cancellation_requested';
ALTER TYPE booking_status ADD VALUE IF NOT EXISTS 'refunded' AFTER 'refund_pending';
