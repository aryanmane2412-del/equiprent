-- name: GetRentalAgreement :one
SELECT * FROM rental_agreements WHERE booking_id = @booking_id AND superseded_at IS NULL;

-- name: CreateReview :one
INSERT INTO reviews (booking_id, reviewer_org_id, reviewer_user_id, subject_type, subject_id, rating, comment)
VALUES (@booking_id, @reviewer_org_id, @reviewer_user_id, @subject_type, @subject_id, @rating, sqlc.narg(comment))
RETURNING *;

-- name: ListSubjectReviews :many
SELECT r.id, r.booking_id, r.subject_type, r.subject_id, r.rating, r.comment, r.created_at, o.display_name AS reviewer_name
FROM reviews r
JOIN organizations o ON o.id = r.reviewer_org_id
WHERE r.subject_type = @subject_type AND r.subject_id = @subject_id AND r.status = 'published'
  AND (sqlc.narg(after)::uuid IS NULL OR r.id < sqlc.narg(after))
ORDER BY r.id DESC
LIMIT @result_limit;

-- name: RefreshEquipmentRating :exec
UPDATE equipment e
SET rating_avg = s.avg_rating, rating_count = s.cnt
FROM (SELECT round(avg(rating), 2)::numeric AS avg_rating, count(*)::int AS cnt
      FROM reviews WHERE subject_type = 'equipment' AND subject_id = @id AND status = 'published') s
WHERE e.id = @id;

-- name: RefreshOrgRating :exec
UPDATE organizations o
SET rating_avg = s.avg_rating, rating_count = s.cnt
FROM (SELECT round(avg(rating), 2)::numeric AS avg_rating, count(*)::int AS cnt
      FROM reviews WHERE subject_type IN ('owner', 'customer') AND subject_id = @id AND status = 'published') s
WHERE o.id = @id;

-- name: RefreshOperatorRating :exec
UPDATE operator_profiles op
SET rating_avg = s.avg_rating, rating_count = s.cnt
FROM (SELECT round(avg(rating), 2)::numeric AS avg_rating, count(*)::int AS cnt
      FROM reviews WHERE subject_type = 'operator' AND subject_id = @id AND status = 'published') s
WHERE op.user_id = @id;

-- name: SetReviewStatus :one
UPDATE reviews SET status = @status WHERE id = @id RETURNING *;

-- name: CreateDispute :one
INSERT INTO disputes (booking_id, raised_by_org_id, raised_by_user_id, against_org_id, category, summary, amount_claimed_paise, conversation_id)
VALUES (@booking_id, @raised_by_org_id, @raised_by_user_id, @against_org_id, @category, @summary, @amount_claimed_paise, sqlc.narg(conversation_id))
RETURNING *;

-- name: GetDispute :one
SELECT * FROM disputes WHERE id = @id;

-- name: ListOrgDisputes :many
SELECT * FROM disputes
WHERE (raised_by_org_id = @org_id OR against_org_id = @org_id)
  AND (sqlc.narg(after)::uuid IS NULL OR id < sqlc.narg(after))
ORDER BY id DESC
LIMIT @result_limit;

-- name: ListDisputeQueue :many
SELECT * FROM disputes
WHERE (sqlc.narg(status)::text IS NULL OR status::text = sqlc.narg(status))
  AND (sqlc.narg(after)::uuid IS NULL OR id < sqlc.narg(after))
ORDER BY id DESC
LIMIT @result_limit;

-- name: AssignDispute :one
UPDATE disputes SET assigned_admin_id = @admin_id, status = 'under_review'
WHERE id = @id AND status IN ('open', 'under_review')
RETURNING *;

-- name: ResolveDispute :one
UPDATE disputes
SET status = @status, resolution = @resolution, resolved_amount_paise = sqlc.narg(resolved_amount_paise),
    resolved_at = now(), assigned_admin_id = COALESCE(assigned_admin_id, @admin_id)
WHERE id = @id AND status IN ('open', 'under_review')
RETURNING *;

-- name: CountOpenDisputes :one
SELECT count(*)::int FROM disputes WHERE booking_id = @booking_id AND status IN ('open', 'under_review');

-- name: BookingCompletedAt :one
SELECT completed_at FROM bookings WHERE id = @id;

-- name: SettleDispute :exec
UPDATE disputes SET refunded_paise = @refunded_paise, unrecovered_paise = @unrecovered_paise WHERE id = @id;

-- name: HeldDepositOfBooking :one
-- The security deposit still held for a booking (0 when none) and the payment that carries it.
SELECT COALESCE(sum(amount_paise), 0)::bigint AS held_paise, COALESCE(max(payment_id::text), '')::text AS payment_id
FROM security_deposits WHERE booking_id = @booking_id AND status = 'held';

-- name: ChargedAgainstDeposit :one
-- What other resolved owner-raised disputes of the booking already claim from the deposit.
SELECT COALESCE(sum(resolved_amount_paise - unrecovered_paise), 0)::bigint AS charged
FROM disputes WHERE booking_id = @booking_id AND id <> @exclude_id AND status = 'resolved'
  AND raised_by_org_id = @owner_org_id AND resolved_amount_paise > 0;
