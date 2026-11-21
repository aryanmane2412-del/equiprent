-- Communication and operations (migration 000020): notification preferences
-- and push bookkeeping, agreement signatures and documents, chat attachments
-- and enquiries, maintenance operations, review moderation.

-- ---- notifications ---------------------------------------------------------------------

-- name: ListNotificationPreferences :many
SELECT category, push FROM notification_preferences WHERE user_id = @user_id ORDER BY category;

-- name: UpsertNotificationPreference :exec
INSERT INTO notification_preferences (user_id, category, push)
VALUES (@user_id, @category, @push)
ON CONFLICT (user_id, category) DO UPDATE SET push = EXCLUDED.push;

-- name: PushMutedCategories :many
SELECT category FROM notification_preferences WHERE user_id = @user_id AND NOT push;

-- name: MarkDevicePushed :exec
UPDATE devices SET last_push_at = now(), last_error = NULL WHERE push_token = @push_token;

-- name: MarkDevicePushFailed :exec
UPDATE devices SET last_error = @last_error WHERE push_token = @push_token;

-- name: DeletePushToken :execrows
-- The push service reported the token as no longer valid.
DELETE FROM devices WHERE push_token = @push_token;

-- name: ListUserDevices :many
SELECT id, platform, app_version, last_seen_at, last_push_at, created_at FROM devices
WHERE user_id = @user_id ORDER BY last_seen_at DESC;

-- name: CreateNotificationForUsers :exec
-- One in-app notification per user.
INSERT INTO notifications (user_id, type, title, body, data, channels)
SELECT u, @type, @title, @body, @data, @channels FROM unnest(@user_ids::uuid[]) AS u;

-- ---- agreements ------------------------------------------------------------------------------

-- name: AgreementContext :one
-- Everything the agreement names, read once when it is first generated.
SELECT b.id AS booking_id, b.reference, b.status, b.with_operator, b.pricing_unit::text AS pricing_unit,
       lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at,
       b.price_snapshot, b.estimated_total_paise, b.deposit_paise, b.payment_mode::text AS payment_mode,
       b.work_description, b.delivery, b.pickup,
       ow.legal_name AS owner_legal_name, ow.display_name AS owner_display_name, ow.gstin AS owner_gstin,
       ow.contact_name AS owner_contact_name, ow.contact_phone AS owner_contact_phone,
       COALESCE(oa.line1 || COALESCE(', ' || oa.line2, '') || ', ' || oa.city || ' ' || oa.pincode, '')::text AS owner_address,
       cu.legal_name AS customer_legal_name, cu.display_name AS customer_display_name, cu.gstin AS customer_gstin,
       cu.contact_name AS customer_contact_name, cu.contact_phone AS customer_contact_phone,
       COALESCE(ca.line1 || COALESCE(', ' || ca.line2, '') || ', ' || ca.city || ' ' || ca.pincode, '')::text AS customer_address,
       COALESCE(sa.label || ': ' || sa.line1 || COALESCE(', ' || sa.line2, '') || ', ' || sa.city || ' ' || sa.pincode, '')::text AS site_address,
       e.title AS equipment_title, e.registration_no, e.serial_no, e.manufacture_year, e.timezone AS equipment_timezone,
       b.owner_org_id, b.customer_org_id,
       e.condition::text AS equipment_condition, e.current_hmr, e.id AS equipment_id, b.final_total_paise, b.final_charges,
       COALESCE(sa.state_code, '')::text AS site_state_code, ow.gst_state_code AS owner_state_code, cu.gst_state_code AS customer_state_code,
       e.fuel_policy::text AS fuel_policy, e.late_fee_per_hour_paise, e.late_grace_minutes,
       m.name AS model_name, mk.name AS make_name, COALESCE(cat.name->>'en', '')::text AS category_name,
       COALESCE(cp.name, '')::text AS cancellation_policy_name, cp.rules AS cancellation_rules
FROM bookings b
JOIN organizations ow ON ow.id = b.owner_org_id
JOIN organizations cu ON cu.id = b.customer_org_id
LEFT JOIN addresses oa ON oa.id = ow.billing_address_id
LEFT JOIN addresses ca ON ca.id = cu.billing_address_id
LEFT JOIN addresses sa ON sa.id = b.site_address_id
JOIN equipment e ON e.id = b.equipment_id
JOIN equipment_models m ON m.id = e.model_id
JOIN equipment_makes mk ON mk.id = m.make_id
JOIN equipment_categories cat ON cat.id = m.category_id
LEFT JOIN cancellation_policies cp ON cp.id = b.cancellation_policy_id
WHERE b.id = @booking_id;

-- name: CreateRentalAgreementV2 :one
INSERT INTO rental_agreements (booking_id, template_version, terms_sha256, snapshot)
VALUES (@booking_id, @template_version, @terms_sha256, @snapshot)
RETURNING id;

-- name: CreateAgreementSignature :one
INSERT INTO agreement_signatures (agreement_id, party, signer_user_id, signer_name, signer_designation, method, provider,
                                  provider_reference, terms_sha256, ip, user_agent, consent_text)
VALUES (@agreement_id, @party, @signer_user_id, @signer_name, sqlc.narg(signer_designation), @method, @provider,
        sqlc.narg(provider_reference), @terms_sha256, sqlc.narg(ip), sqlc.narg(user_agent), sqlc.narg(consent_text))
RETURNING *;

-- name: ListAgreementSignatures :many
SELECT * FROM agreement_signatures WHERE agreement_id = @agreement_id ORDER BY signed_at;

-- name: CreateSigningSession :one
INSERT INTO agreement_signing_sessions (agreement_id, party, signer_user_id, signer_name, provider, provider_reference, terms_sha256)
VALUES (@agreement_id, @party, @signer_user_id, @signer_name, @provider, @provider_reference, @terms_sha256)
RETURNING *;

-- name: GetSigningSessionByReference :one
SELECT * FROM agreement_signing_sessions WHERE provider = @provider AND provider_reference = @provider_reference FOR UPDATE;

-- name: FinishSigningSession :exec
UPDATE agreement_signing_sessions SET status = @status, completed_at = now() WHERE id = @id AND status = 'pending';

-- name: GetAgreementDocument :one
SELECT d.id, d.kind, d.sha256, d.created_at, u.object_key, u.size_bytes
FROM agreement_documents d JOIN uploads u ON u.id = d.upload_id
WHERE d.agreement_id = @agreement_id AND d.kind = @kind;

-- name: CreateAgreementDocument :exec
INSERT INTO agreement_documents (agreement_id, kind, upload_id, sha256)
VALUES (@agreement_id, @kind, @upload_id, @sha256)
ON CONFLICT (agreement_id, kind) DO NOTHING;

-- name: ListAgreementDocuments :many
SELECT d.kind, d.sha256, d.created_at, u.size_bytes
FROM agreement_documents d JOIN uploads u ON u.id = d.upload_id
WHERE d.agreement_id = @agreement_id ORDER BY d.created_at;

-- ---- chat ------------------------------------------------------------------------------------

-- name: CreateChatMessage :one
-- client_id makes offline resends idempotent per sender.
INSERT INTO messages (conversation_id, sender_user_id, body, attachment_object_key, attachment_upload_id, client_id)
VALUES (@conversation_id, sqlc.narg(sender_user_id), sqlc.narg(body), sqlc.narg(attachment_object_key),
        sqlc.narg(attachment_upload_id), sqlc.narg(client_id))
ON CONFLICT (sender_user_id, client_id) WHERE client_id IS NOT NULL DO UPDATE SET client_id = EXCLUDED.client_id
RETURNING id, conversation_id, sender_user_id, body, attachment_upload_id, created_at, deleted_at, (xmax = 0)::bool AS inserted;

-- name: ListChatMessages :many
-- Newest first, older than `before`; or oldest first, newer than `after` (polling).
SELECT m.id, m.conversation_id, m.sender_user_id, m.body, m.attachment_upload_id, m.created_at, m.edited_at, m.deleted_at,
       u.full_name AS sender_name, up.content_type AS attachment_content_type, up.object_key AS attachment_key
FROM messages m
LEFT JOIN users u ON u.id = m.sender_user_id
LEFT JOIN uploads up ON up.id = m.attachment_upload_id
WHERE m.conversation_id = @conversation_id
  AND (sqlc.narg(before)::uuid IS NULL OR m.id < sqlc.narg(before))
  AND (sqlc.narg(after)::uuid IS NULL OR m.id > sqlc.narg(after))
ORDER BY CASE WHEN sqlc.narg(after)::uuid IS NULL THEN NULL ELSE m.id END ASC,
         m.id DESC
LIMIT @result_limit;

-- name: GetChatMessage :one
SELECT m.id, m.conversation_id, m.sender_user_id, m.created_at, m.deleted_at, up.object_key AS attachment_key,
       up.content_type AS attachment_content_type
FROM messages m
LEFT JOIN uploads up ON up.id = m.attachment_upload_id
WHERE m.id = @id AND m.conversation_id = @conversation_id;

-- name: SoftDeleteChatMessage :execrows
UPDATE messages SET deleted_at = now()
WHERE id = @id AND conversation_id = @conversation_id AND sender_user_id = @sender_user_id AND deleted_at IS NULL
  AND created_at > now() - make_interval(mins => @window_minutes::int);

-- name: ListConversationParticipantIDs :many
SELECT user_id FROM conversation_participants WHERE conversation_id = @conversation_id;

-- name: ListUserConversationsV2 :many
SELECT c.id, c.kind, c.booking_id, c.equipment_id, c.subject, c.updated_at,
       COALESCE((SELECT CASE WHEN m.attachment_upload_id IS NOT NULL AND m.body IS NULL THEN '[photo]' ELSE m.body END
                 FROM messages m WHERE m.conversation_id = c.id AND m.deleted_at IS NULL
                 ORDER BY m.id DESC LIMIT 1), '')::text AS last_message,
       (SELECT count(*) FROM messages m WHERE m.conversation_id = c.id AND m.deleted_at IS NULL
         AND m.created_at > COALESCE(p.last_read_at, '-infinity'::timestamptz)
         AND m.sender_user_id IS DISTINCT FROM @user_id)::int AS unread,
       COALESCE(b.reference, '')::text AS booking_reference, COALESCE(b.status::text, '')::text AS booking_status
FROM conversation_participants p
JOIN conversations c ON c.id = p.conversation_id
LEFT JOIN bookings b ON b.id = c.booking_id
WHERE p.user_id = @user_id
ORDER BY c.updated_at DESC
LIMIT @result_limit;

-- name: CountUnreadMessages :one
SELECT COALESCE(sum((SELECT count(*) FROM messages m WHERE m.conversation_id = p.conversation_id AND m.deleted_at IS NULL
         AND m.created_at > COALESCE(p.last_read_at, '-infinity'::timestamptz)
         AND m.sender_user_id IS DISTINCT FROM @user_id)), 0)::int AS unread
FROM conversation_participants p WHERE p.user_id = @user_id;

-- name: GetEnquiryConversation :one
SELECT * FROM conversations WHERE equipment_id = @equipment_id AND customer_org_id = @customer_org_id;

-- name: CreateEnquiryConversation :one
INSERT INTO conversations (kind, subject, equipment_id, customer_org_id, owner_org_id)
VALUES ('direct', @subject, @equipment_id, @customer_org_id, @owner_org_id)
RETURNING *;

-- name: EquipmentForEnquiry :one
SELECT id, owner_org_id, title, status::text AS status FROM equipment WHERE id = @id AND deleted_at IS NULL;

-- ---- maintenance -----------------------------------------------------------------------------

-- name: CreateMaintenanceRecordV2 :one
INSERT INTO maintenance_records (equipment_id, plan_id, kind, task, status, scheduled_for, performed_at, hmr_at, cost_paise,
                                 parts_cost_paise, labour_cost_paise, vendor, notes, recorded_by, downtime, invoice_upload_id)
VALUES (@equipment_id, sqlc.narg(plan_id), @kind, @task, @status, sqlc.narg(scheduled_for), sqlc.narg(performed_at),
        sqlc.narg(hmr_at), sqlc.narg(cost_paise), sqlc.narg(parts_cost_paise), sqlc.narg(labour_cost_paise), sqlc.narg(vendor),
        sqlc.narg(notes), sqlc.narg(recorded_by),
        CASE WHEN sqlc.narg(downtime_start)::timestamptz IS NULL THEN NULL
             ELSE tstzrange(sqlc.narg(downtime_start)::timestamptz, sqlc.narg(downtime_end)::timestamptz, '[)') END,
        sqlc.narg(invoice_upload_id))
RETURNING id;

-- name: GetMaintenanceRecordView :one
SELECT r.id, r.equipment_id, e.title AS equipment_title, r.plan_id, r.kind, r.task, r.status, r.scheduled_for, r.performed_at,
       r.hmr_at, r.cost_paise, r.parts_cost_paise, r.labour_cost_paise, r.vendor, r.notes,
       r.downtime,
       r.availability_block_id, r.invoice_upload_id, r.created_at, r.updated_at
FROM maintenance_records r JOIN equipment e ON e.id = r.equipment_id
WHERE r.id = @id AND r.equipment_id = @equipment_id;

-- name: ListMaintenanceHistory :many
-- Records of the owner's machines, newest first, with optional filters.
SELECT r.id, r.equipment_id, e.title AS equipment_title, r.plan_id, r.kind, r.task, r.status, r.scheduled_for, r.performed_at,
       r.hmr_at, r.cost_paise, r.parts_cost_paise, r.labour_cost_paise, r.vendor, r.notes,
       r.downtime,
       r.availability_block_id, r.invoice_upload_id, r.created_at, r.updated_at
FROM maintenance_records r JOIN equipment e ON e.id = r.equipment_id
WHERE e.owner_org_id = @owner_org_id AND e.deleted_at IS NULL
  AND (sqlc.narg(equipment_id)::uuid IS NULL OR r.equipment_id = sqlc.narg(equipment_id))
  AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR r.equipment_id = ANY (sqlc.narg(equipment_ids)::uuid[]))
  AND (sqlc.narg(kind)::text IS NULL OR r.kind::text = sqlc.narg(kind))
  AND (sqlc.narg(status)::text IS NULL OR r.status::text = sqlc.narg(status))
  AND (sqlc.narg(after)::uuid IS NULL OR r.id < sqlc.narg(after))
ORDER BY r.id DESC
LIMIT @result_limit;

-- name: UpdateMaintenanceRecordV2 :one
-- Status changes plus editable details. performed_at follows the status.
UPDATE maintenance_records
SET status            = @status,
    performed_at      = CASE WHEN @status::maintenance_status = 'done' THEN COALESCE(sqlc.narg(performed_at), performed_at, now()) ELSE NULL END,
    scheduled_for     = COALESCE(sqlc.narg(scheduled_for), scheduled_for),
    hmr_at            = COALESCE(sqlc.narg(hmr_at), hmr_at),
    cost_paise        = COALESCE(sqlc.narg(cost_paise), cost_paise),
    parts_cost_paise  = COALESCE(sqlc.narg(parts_cost_paise), parts_cost_paise),
    labour_cost_paise = COALESCE(sqlc.narg(labour_cost_paise), labour_cost_paise),
    vendor            = COALESCE(sqlc.narg(vendor), vendor),
    notes             = COALESCE(sqlc.narg(notes), notes),
    invoice_upload_id = COALESCE(sqlc.narg(invoice_upload_id), invoice_upload_id),
    downtime          = CASE WHEN @clear_downtime::bool THEN NULL
                             WHEN sqlc.narg(downtime_start)::timestamptz IS NULL THEN downtime
                             ELSE tstzrange(sqlc.narg(downtime_start)::timestamptz, sqlc.narg(downtime_end)::timestamptz, '[)') END,
    reminded_at       = CASE WHEN sqlc.narg(scheduled_for)::date IS NULL THEN reminded_at ELSE NULL END,
    overdue_notified_at = CASE WHEN sqlc.narg(scheduled_for)::date IS NULL THEN overdue_notified_at ELSE NULL END
WHERE id = @id AND equipment_id = @equipment_id AND status IN ('scheduled', 'in_progress')
RETURNING id;

-- name: SetMaintenanceBlock :exec
UPDATE maintenance_records SET availability_block_id = sqlc.narg(block_id) WHERE id = @id;

-- name: CreateMaintenanceBlock :one
INSERT INTO availability_blocks (equipment_id, period, reason, note, created_by)
VALUES (@equipment_id, tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'), 'maintenance', sqlc.narg(note), sqlc.narg(created_by))
RETURNING id;

-- name: DeleteMaintenanceBlock :exec
DELETE FROM availability_blocks WHERE id = @id AND reason = 'maintenance';

-- name: TrimMaintenanceBlock :exec
-- A job finished early: free the rest of the blocked period (or all of it,
-- if it had not started).
UPDATE availability_blocks
SET period = tstzrange(lower(period), @ends_at::timestamptz, '[)')
WHERE id = @id AND reason = 'maintenance' AND lower(period) < @ends_at::timestamptz AND upper(period) > @ends_at::timestamptz;

-- name: DeleteFutureMaintenanceBlock :exec
DELETE FROM availability_blocks WHERE id = @id AND reason = 'maintenance' AND lower(period) >= @from_at::timestamptz;

-- name: MaintenanceConflicts :many
-- Slot-holding bookings that overlap a proposed downtime.
SELECT reference, lower(period)::timestamptz AS starts_at, upper(period)::timestamptz AS ends_at
FROM bookings
WHERE equipment_id = @equipment_id AND booking_holds_slot(status, resume_status)
  AND period && tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)')
ORDER BY lower(period)
LIMIT 5;

-- name: UpdateMaintenancePlan :one
UPDATE maintenance_plans
SET task = COALESCE(sqlc.narg(task), task),
    every_hours = CASE WHEN @clear_hours::bool THEN NULL ELSE COALESCE(sqlc.narg(every_hours), every_hours) END,
    every_days  = CASE WHEN @clear_days::bool THEN NULL ELSE COALESCE(sqlc.narg(every_days), every_days) END,
    is_active   = COALESCE(sqlc.narg(is_active), is_active)
WHERE id = @id AND equipment_id = @equipment_id
RETURNING *;

-- name: MaintenanceCostSummary :many
-- Completed work in a window, grouped by machine, kind and month.
SELECT r.equipment_id, e.title AS equipment_title, r.kind::text AS kind,
       to_char(date_trunc('month', r.performed_at AT TIME ZONE 'Asia/Kolkata'), 'YYYY-MM')::text AS month,
       count(*)::int AS jobs,
       COALESCE(sum(r.cost_paise), 0)::bigint AS cost_paise,
       COALESCE(sum(r.parts_cost_paise), 0)::bigint AS parts_cost_paise,
       COALESCE(sum(r.labour_cost_paise), 0)::bigint AS labour_cost_paise,
       COALESCE(sum(EXTRACT(EPOCH FROM (upper(r.downtime) - lower(r.downtime))) / 3600), 0)::float8 AS downtime_hours
FROM maintenance_records r JOIN equipment e ON e.id = r.equipment_id
WHERE e.owner_org_id = @owner_org_id AND r.status = 'done'
  AND r.performed_at >= @from_at::timestamptz AND r.performed_at < @to_at::timestamptz
  AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR r.equipment_id = ANY (sqlc.narg(equipment_ids)::uuid[]))
GROUP BY r.equipment_id, e.title, r.kind, month
ORDER BY month, e.title, kind;

-- name: UpcomingMaintenanceReminders :many
-- Scheduled jobs within the reminder window not yet reminded today.
SELECT r.id, r.equipment_id, r.task, r.kind::text AS kind, r.scheduled_for, e.title, e.owner_org_id
FROM maintenance_records r JOIN equipment e ON e.id = r.equipment_id AND e.deleted_at IS NULL
WHERE r.status = 'scheduled' AND r.scheduled_for IS NOT NULL
  AND r.scheduled_for >= @today::date AND r.scheduled_for <= @until::date
  AND (r.reminded_at IS NULL OR r.reminded_at < @today::date)
ORDER BY r.scheduled_for
LIMIT 500;

-- name: MarkMaintenanceReminded :exec
UPDATE maintenance_records SET reminded_at = now() WHERE id = ANY (@ids::uuid[]);

-- name: OverdueMaintenanceRecords :many
SELECT r.id, r.equipment_id, r.task, r.scheduled_for, e.title, e.owner_org_id
FROM maintenance_records r JOIN equipment e ON e.id = r.equipment_id AND e.deleted_at IS NULL
WHERE r.status = 'scheduled' AND r.scheduled_for < @today::date AND r.overdue_notified_at IS NULL
ORDER BY r.scheduled_for
LIMIT 500;

-- name: MarkMaintenanceOverdueNotified :exec
UPDATE maintenance_records SET overdue_notified_at = now() WHERE id = ANY (@ids::uuid[]);

-- name: DuePlansToRemind :many
-- Due plans not reminded in the last week (calendar or hour meter).
SELECT p.id AS plan_id, p.task, p.equipment_id, e.title, e.owner_org_id
FROM maintenance_plans p
JOIN equipment e ON e.id = p.equipment_id AND e.deleted_at IS NULL
WHERE p.is_active
  AND (p.last_reminded_at IS NULL OR p.last_reminded_at < @remind_before::timestamptz)
  AND ((p.every_days IS NOT NULL AND COALESCE(p.last_done_at, p.created_at) + make_interval(days => p.every_days) <= now())
    OR (p.every_hours IS NOT NULL AND e.current_hmr >= COALESCE(p.last_done_hmr, 0) + p.every_hours))
LIMIT 500;

-- name: MarkPlansReminded :exec
UPDATE maintenance_plans SET last_reminded_at = now() WHERE id = ANY (@ids::uuid[]);

-- ---- reviews ---------------------------------------------------------------------------------

-- name: CreateReviewV2 :one
INSERT INTO reviews (booking_id, reviewer_org_id, reviewer_user_id, subject_type, subject_id, rating, comment, status, screening_flags)
VALUES (@booking_id, @reviewer_org_id, @reviewer_user_id, @subject_type, @subject_id, @rating, sqlc.narg(comment), @status, @screening_flags)
RETURNING *;

-- name: ListPublishedReviews :many
SELECT r.id, r.booking_id, r.subject_type, r.subject_id, r.rating, r.comment, r.created_at, r.reply, r.replied_at,
       o.display_name AS reviewer_name, e.title AS equipment_title
FROM reviews r
JOIN organizations o ON o.id = r.reviewer_org_id
LEFT JOIN equipment e ON r.subject_type = 'equipment' AND e.id = r.subject_id
WHERE r.subject_type = @subject_type AND r.subject_id = @subject_id AND r.status = 'published'
  AND (sqlc.narg(after)::uuid IS NULL OR r.id < sqlc.narg(after))
ORDER BY r.id DESC
LIMIT @result_limit;

-- name: ReviewSummary :many
SELECT rating, count(*)::int AS cnt FROM reviews
WHERE subject_type = @subject_type AND subject_id = @subject_id AND status = 'published'
GROUP BY rating;

-- name: ListOwnerReceivedReviews :many
-- Published reviews of the organisation and of its machines.
SELECT r.id, r.booking_id, r.subject_type, r.subject_id, r.rating, r.comment, r.created_at, r.reply, r.replied_at,
       r.status, o.display_name AS reviewer_name, e.title AS equipment_title
FROM reviews r
JOIN organizations o ON o.id = r.reviewer_org_id
LEFT JOIN equipment e ON r.subject_type = 'equipment' AND e.id = r.subject_id
WHERE r.status = 'published'
  AND ((r.subject_type = 'owner' AND r.subject_id = @org_id) OR (r.subject_type = 'equipment' AND e.owner_org_id = @org_id))
  AND (sqlc.narg(after)::uuid IS NULL OR r.id < sqlc.narg(after))
ORDER BY r.id DESC
LIMIT @result_limit;

-- name: ListBookingReviews :many
SELECT r.id, r.booking_id, r.reviewer_org_id, r.subject_type, r.subject_id, r.rating, r.comment, r.status, r.created_at,
       r.reply, r.replied_at, o.display_name AS reviewer_name
FROM reviews r JOIN organizations o ON o.id = r.reviewer_org_id
WHERE r.booking_id = @booking_id
ORDER BY r.created_at;

-- name: GetReview :one
SELECT r.*, e.owner_org_id AS equipment_owner_org_id
FROM reviews r
LEFT JOIN equipment e ON r.subject_type = 'equipment' AND e.id = r.subject_id
WHERE r.id = @id;

-- name: SetReviewReply :one
UPDATE reviews SET reply = @reply, reply_by = @reply_by, replied_at = now()
WHERE id = @id AND reply IS NULL
RETURNING *;

-- name: CreateReviewReport :execrows
INSERT INTO review_reports (review_id, reporter_user_id, reason, note)
VALUES (@review_id, @reporter_user_id, @reason, sqlc.narg(note))
ON CONFLICT DO NOTHING;

-- name: BumpReviewReports :one
-- Counts a report; enough reports hide a published review until moderated.
UPDATE reviews
SET report_count = report_count + 1,
    status = CASE WHEN status = 'published' AND report_count + 1 >= @hide_threshold::int THEN 'flagged' ELSE status END
WHERE id = @id
RETURNING status, report_count;

-- name: ModerateReview :one
UPDATE reviews
SET status = @status, moderated_by = @moderated_by, moderated_at = now(), moderation_note = @moderation_note
WHERE id = @id
RETURNING *;

-- name: AdminListReviews :many
SELECT r.id, r.booking_id, r.subject_type, r.subject_id, r.rating, r.comment, r.status, r.screening_flags, r.report_count,
       r.created_at, r.moderated_at, r.moderation_note, o.display_name AS reviewer_name, u.full_name AS reviewer_user_name,
       COALESCE(e.title, so.display_name, ou.full_name, '')::text AS subject_name,
       COALESCE((SELECT array_agg(DISTINCT rr.reason) FROM review_reports rr WHERE rr.review_id = r.id), '{}')::text[] AS report_reasons
FROM reviews r
JOIN organizations o ON o.id = r.reviewer_org_id
JOIN users u ON u.id = r.reviewer_user_id
LEFT JOIN equipment e ON r.subject_type = 'equipment' AND e.id = r.subject_id
LEFT JOIN organizations so ON r.subject_type IN ('owner', 'customer') AND so.id = r.subject_id
LEFT JOIN users ou ON r.subject_type = 'operator' AND ou.id = r.subject_id
WHERE (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status))
  AND (NOT @reported_only::bool OR r.report_count > 0)
  AND (sqlc.narg(after)::uuid IS NULL OR r.id < sqlc.narg(after))
ORDER BY r.id DESC
LIMIT @result_limit;
