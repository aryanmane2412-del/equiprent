-- name: ListBusyRanges :many
-- Busy ranges only (no booking details) for the public calendar.
SELECT starts_at, ends_at, kind FROM (
  SELECT lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at, 'booked'::text AS kind
  FROM bookings b
  WHERE b.equipment_id = @equipment_id AND booking_holds_slot(b.status)
    AND b.period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
  UNION ALL
  SELECT lower(a.period)::timestamptz, upper(a.period)::timestamptz, 'blocked'::text
  FROM availability_blocks a
  WHERE a.equipment_id = @equipment_id
    AND a.period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
) busy
ORDER BY starts_at;

-- name: ListAvailabilityBlocks :many
SELECT id, lower(period)::timestamptz AS starts_at, upper(period)::timestamptz AS ends_at, reason, note, created_at
FROM availability_blocks
WHERE equipment_id = @equipment_id
  AND period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
ORDER BY lower(period);

-- name: DeleteAvailabilityBlock :execrows
DELETE FROM availability_blocks WHERE id = @id AND equipment_id = @equipment_id;

-- name: ListEquipmentBookingsInRange :many
SELECT id, reference, status, lower(period)::timestamptz AS starts_at, upper(period)::timestamptz AS ends_at
FROM bookings
WHERE equipment_id = @equipment_id
  AND booking_holds_slot(status) -- booked only: requests awaiting the owner do not hold dates
  AND period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
ORDER BY lower(period);
