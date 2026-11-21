-- ---- uploads ---------------------------------------------------------------------

-- name: CreateUpload :one
INSERT INTO uploads (user_id, purpose, object_key, content_type, size_bytes, sha256, original_name)
VALUES (@user_id, @purpose, @object_key, @content_type, @size_bytes, @sha256, sqlc.narg(original_name))
RETURNING *;

-- name: GetUpload :one
SELECT * FROM uploads WHERE id = @id;

-- name: GetUploadByKey :one
SELECT * FROM uploads WHERE object_key = @object_key;

-- name: MarkUploadAttached :exec
UPDATE uploads SET attached_at = COALESCE(attached_at, now()) WHERE id = @id;

-- name: CountRecentUploads :one
SELECT count(*)::int FROM uploads WHERE user_id = @user_id AND created_at > now() - interval '1 hour';

-- ---- documents (kyc_records) -------------------------------------------------------

-- name: CreateDocument :one
INSERT INTO kyc_records (organization_id, user_id, doc_type, number_enc, number_hash, number_last4,
                         document_object_key, document_sha256, content_type, upload_id, context, expires_on, status)
VALUES (sqlc.narg(organization_id), sqlc.narg(user_id), @doc_type, sqlc.narg(number_enc), sqlc.narg(number_hash), sqlc.narg(number_last4),
        @document_object_key, @document_sha256, @content_type, @upload_id, @context, sqlc.narg(expires_on), 'submitted')
RETURNING *;

-- name: GetDocument :one
SELECT * FROM kyc_records WHERE id = @id;

-- name: ListSubjectDocuments :many
SELECT * FROM kyc_records
WHERE context = @context
  AND (organization_id IS NOT DISTINCT FROM sqlc.narg(organization_id))
  AND (user_id IS NOT DISTINCT FROM sqlc.narg(user_id))
ORDER BY created_at;

-- name: DeleteDocument :execrows
DELETE FROM kyc_records WHERE id = @id AND status IN ('submitted', 'rejected', 'pending');

-- name: ReviewDocument :one
UPDATE kyc_records SET status = @status, reviewed_by = @reviewed_by, reviewed_at = now(), review_note = sqlc.narg(review_note)
WHERE id = @id
RETURNING *;

-- name: ApproveSubmittedDocuments :exec
UPDATE kyc_records SET status = 'verified', reviewed_by = @reviewed_by, reviewed_at = now()
WHERE context = @context AND status IN ('submitted', 'pending')
  AND (organization_id IS NOT DISTINCT FROM sqlc.narg(organization_id))
  AND (user_id IS NOT DISTINCT FROM sqlc.narg(user_id));

-- ---- applications --------------------------------------------------------------------

-- name: EnsureApplication :one
INSERT INTO verification_applications (role, organization_id, user_id)
VALUES (@role, sqlc.narg(organization_id), sqlc.narg(user_id))
ON CONFLICT (role, (COALESCE(organization_id, user_id))) DO UPDATE SET role = EXCLUDED.role
RETURNING *;

-- name: GetApplication :one
SELECT a.*,
       COALESCE(o.display_name, u.full_name, '')::text AS subject_name,
       COALESCE(o.legal_name, '')::text AS legal_name
FROM verification_applications a
LEFT JOIN organizations o ON o.id = a.organization_id
LEFT JOIN users u ON u.id = a.user_id
WHERE a.id = @id;

-- name: SubmitApplication :one
UPDATE verification_applications
SET status = 'submitted', submitted_by = @submitted_by, submitted_at = now(),
    reviewer_id = NULL, reviewed_at = NULL, rejection_reason = NULL
WHERE id = @id AND status IN ('draft', 'rejected')
RETURNING *;

-- name: StartApplicationReview :one
UPDATE verification_applications SET status = 'under_review', reviewer_id = @reviewer_id
WHERE id = @id AND status = 'submitted'
RETURNING *;

-- name: DecideApplication :one
-- Approve or reject a submitted application, or revoke an approved one.
UPDATE verification_applications
SET status = @status, reviewer_id = @reviewer_id, reviewed_at = now(), rejection_reason = sqlc.narg(rejection_reason)
WHERE id = @id
  AND ((@status::application_status = 'approved' AND status IN ('submitted', 'under_review'))
    OR (@status::application_status = 'rejected' AND status IN ('submitted', 'under_review', 'approved')))
RETURNING *;

-- name: ListApplications :many
SELECT a.id, a.role, a.status, a.organization_id, a.user_id, a.submitted_at, a.reviewed_at, a.updated_at,
       COALESCE(o.display_name, u.full_name, '')::text AS subject_name
FROM verification_applications a
LEFT JOIN organizations o ON o.id = a.organization_id
LEFT JOIN users u ON u.id = a.user_id
WHERE (sqlc.narg(status)::text IS NULL OR a.status::text = sqlc.narg(status))
  AND (sqlc.narg(role)::text IS NULL OR a.role = sqlc.narg(role))
  AND a.status <> 'draft'
  AND (sqlc.narg(after)::uuid IS NULL OR a.id < sqlc.narg(after))
ORDER BY a.id DESC
LIMIT @result_limit;

-- name: InsertVerificationEvent :exec
INSERT INTO verification_events (application_id, actor_user_id, action, document_id, reason)
VALUES (@application_id, sqlc.narg(actor_user_id), @action, sqlc.narg(document_id), sqlc.narg(reason));

-- name: ListVerificationEvents :many
SELECT e.id, e.actor_user_id, e.action, e.document_id, e.reason, e.created_at,
       COALESCE(u.full_name, '')::text AS actor_name
FROM verification_events e
LEFT JOIN users u ON u.id = e.actor_user_id
WHERE e.application_id = @application_id
ORDER BY e.id;

-- ---- owner business profile -----------------------------------------------------------

-- name: UpdateOwnerBusiness :one
UPDATE organizations
SET legal_name = @legal_name, display_name = @display_name, org_type = @org_type,
    gstin = sqlc.narg(gstin), gst_state_code = sqlc.narg(gst_state_code),
    pan_enc = sqlc.narg(pan_enc), pan_hash = sqlc.narg(pan_hash), pan_last4 = sqlc.narg(pan_last4),
    udyam_number = sqlc.narg(udyam_number), cin = sqlc.narg(cin),
    contact_name = @contact_name, contact_phone = @contact_phone, contact_email = @contact_email::citext,
    billing_address_id = sqlc.narg(billing_address_id), is_supplier = true
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: SetOrgKYCStatus :exec
UPDATE organizations
SET kyc_status = @kyc_status,
    kyc_verified_at = CASE WHEN @kyc_status::verification_status = 'verified' THEN now() ELSE NULL END
WHERE id = @id;

-- name: GetOrgKYCStatus :one
SELECT kyc_status FROM organizations WHERE id = @id;

-- name: RetireBankAccounts :exec
UPDATE bank_accounts SET deleted_at = now(), is_primary = false
WHERE organization_id = @organization_id AND deleted_at IS NULL;

-- name: InsertBankAccount :one
INSERT INTO bank_accounts (organization_id, account_holder_name, account_number_enc, account_number_hash, account_last4,
                           ifsc, gateway_linked_account_id, verification_status, verified_at, is_primary)
VALUES (@organization_id, @account_holder_name, @account_number_enc, @account_number_hash, @account_last4,
        @ifsc, sqlc.narg(gateway_linked_account_id), @verification_status, sqlc.narg(verified_at), true)
RETURNING id, account_holder_name, account_last4, ifsc, gateway_linked_account_id, verification_status, verified_at, created_at;

-- name: GetPrimaryBankAccount :one
SELECT id, account_holder_name, account_last4, ifsc, gateway_linked_account_id, verification_status, verified_at, created_at
FROM bank_accounts WHERE organization_id = @organization_id AND is_primary AND deleted_at IS NULL;

-- ---- operator ------------------------------------------------------------------------------

-- name: UpsertOperatorVerificationProfile :one
INSERT INTO operator_profiles (user_id, licence_number_enc, licence_number_hash, licence_last4, licence_class,
                               licence_expires_on, experience_years, availability)
VALUES (@user_id, @licence_number_enc, @licence_number_hash, @licence_last4, @licence_class,
        @licence_expires_on, @experience_years, @availability)
ON CONFLICT (user_id) DO UPDATE
SET licence_number_enc = EXCLUDED.licence_number_enc, licence_number_hash = EXCLUDED.licence_number_hash,
    licence_last4 = EXCLUDED.licence_last4, licence_class = EXCLUDED.licence_class,
    licence_expires_on = EXCLUDED.licence_expires_on, experience_years = EXCLUDED.experience_years,
    availability = EXCLUDED.availability
RETURNING *;

-- name: SetOperatorVerified :exec
UPDATE operator_profiles
SET verified_at = CASE WHEN @verified::bool THEN now() ELSE NULL END
WHERE user_id = @user_id;

-- name: OperatorVerified :one
SELECT (verified_at IS NOT NULL)::bool AS verified FROM operator_profiles WHERE user_id = @user_id;

-- name: OperatorEligibleForEquipment :one
SELECT EXISTS (
  SELECT 1 FROM operator_profiles op
  JOIN operator_skills sk ON sk.operator_user_id=op.user_id
  JOIN equipment e ON e.id = @equipment_id
  JOIN equipment_models m ON m.id=e.model_id AND m.category_id=sk.category_id
  WHERE op.user_id = @operator_user_id AND op.verified_at IS NOT NULL
    AND op.licence_expires_on >= CURRENT_DATE
) AS ok
FROM (SELECT @operator_user_id::uuid AS operator_user_id, @equipment_id::uuid AS equipment_id) AS input;

-- name: CreateQualification :one
INSERT INTO operator_qualifications (operator_user_id, title, issuer, issued_on, valid_until, document_id)
VALUES (@operator_user_id, @title, sqlc.narg(issuer), sqlc.narg(issued_on), sqlc.narg(valid_until), sqlc.narg(document_id))
RETURNING *;

-- name: ListQualifications :many
SELECT * FROM operator_qualifications WHERE operator_user_id = @operator_user_id ORDER BY created_at;

-- name: DeleteQualification :execrows
DELETE FROM operator_qualifications WHERE id = @id AND operator_user_id = @operator_user_id;

-- ---- customer -----------------------------------------------------------------------------

-- name: SetUserAvatar :exec
UPDATE users SET avatar_object_key = @avatar_object_key WHERE id = @id;

-- name: CountOrgAddresses :one
SELECT count(*)::int FROM addresses WHERE organization_id = @organization_id AND deleted_at IS NULL;

-- ---- fleet managers -------------------------------------------------------------------------

-- name: EnsureFleetManagerProfile :exec
INSERT INTO fleet_manager_profiles (user_id, organization_id)
VALUES (@user_id, @organization_id)
ON CONFLICT (user_id) DO NOTHING;

-- name: GetFleetManagerProfile :one
SELECT f.user_id, f.organization_id, f.can_set_pricing, f.all_equipment, f.created_at,
       o.display_name AS organization_name, o.legal_name, o.kyc_status, o.contact_name, o.contact_phone, o.contact_email
FROM fleet_manager_profiles f
JOIN organizations o ON o.id = f.organization_id
WHERE f.user_id = @user_id;

-- name: SetFleetManagerAccess :exec
UPDATE fleet_manager_profiles SET all_equipment = @all_equipment WHERE user_id = @user_id AND organization_id = @organization_id;

-- name: DeleteFleetGrants :exec
DELETE FROM fleet_manager_equipment WHERE user_id = @user_id AND organization_id = @organization_id;

-- name: AddFleetGrants :execrows
-- Only the organisation's own, non-retired equipment can be granted.
INSERT INTO fleet_manager_equipment (user_id, organization_id, equipment_id, granted_by)
SELECT @user_id, @organization_id, e.id, @granted_by
FROM equipment e
WHERE e.id = ANY(@equipment_ids::uuid[]) AND e.owner_org_id = @organization_id AND e.deleted_at IS NULL
ON CONFLICT (user_id, equipment_id) DO NOTHING;

-- name: ListFleetGrants :many
SELECT g.equipment_id, e.title, e.status, g.granted_at
FROM fleet_manager_equipment g
JOIN equipment e ON e.id = g.equipment_id AND e.deleted_at IS NULL
WHERE g.user_id = @user_id AND g.organization_id = @organization_id
ORDER BY e.title;

-- name: EquipmentOwnedByOrg :one
SELECT EXISTS (SELECT 1 FROM equipment WHERE id = @equipment_id AND owner_org_id = @org_id)::bool AS owned;

-- name: FleetManagerEquipmentAccess :one
-- Direct grants and fleet membership both count (fleet_manager_scope); a
-- fleet-management restriction removes all access.
SELECT (COALESCE((SELECT f.all_equipment FROM fleet_manager_profiles f WHERE f.user_id = @user_id AND f.organization_id = @organization_id), false)
        AND NOT is_restricted(@user_id, 'fleet_management'))::bool AS all_equipment,
       EXISTS (SELECT 1 FROM fleet_manager_scope s WHERE s.user_id = @user_id AND s.equipment_id = @equipment_id AND s.organization_id = @organization_id) AS granted;

-- name: FleetManagerScopeIDs :many
SELECT DISTINCT s.equipment_id FROM fleet_manager_scope s WHERE s.user_id = @user_id AND s.organization_id = @organization_id;

-- name: ListOrgFleetManagers :many
SELECT u.id AS user_id, u.full_name, u.phone, COALESCE(f.all_equipment, false)::bool AS all_equipment,
       (SELECT count(*) FROM fleet_manager_equipment g WHERE g.user_id = u.id AND g.organization_id = @organization_id)::int AS granted_count
FROM user_roles ur
JOIN users u ON u.id = ur.user_id AND u.deleted_at IS NULL
LEFT JOIN fleet_manager_profiles f ON f.user_id = u.id AND f.organization_id = @organization_id
WHERE ur.organization_id = @organization_id AND ur.role_code = 'fleet_manager' AND ur.revoked_at IS NULL
ORDER BY u.full_name;

-- name: InsertDocumentAccessAudit :exec
INSERT INTO audit_logs (actor_user_id, actor_role, action, entity_type, entity_id, reason, request_id, after)
VALUES (@actor_user_id, @actor_role, 'document.url_issued', 'kyc_records', @entity_id, sqlc.narg(reason), sqlc.narg(request_id), sqlc.narg(after));

-- name: ReopenApplication :one
-- Send a submitted or approved application back to draft so its owner can edit it; it must be
-- submitted and reviewed again.
UPDATE verification_applications
SET status = 'draft', reviewer_id = NULL, reviewed_at = NULL, rejection_reason = NULL
WHERE id = @id AND status IN ('submitted', 'under_review', 'approved')
RETURNING *;
