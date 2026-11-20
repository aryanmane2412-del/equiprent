-- name: CreateUser :one
INSERT INTO users (phone, full_name, email, preferred_language)
VALUES (@phone, @full_name, sqlc.narg(email), @preferred_language)
RETURNING *;

-- name: GetUserByPhone :one
SELECT * FROM users WHERE phone = @phone AND deleted_at IS NULL;

-- name: GetUser :one
SELECT * FROM users WHERE id = @id;

-- name: SoftDeleteUser :execrows
-- Releases the phone/email for re-registration; the row is kept for history.
UPDATE users SET status = 'deleted', deleted_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: CreateOrganization :one
INSERT INTO organizations (legal_name, display_name, org_type, is_buyer, is_supplier, gstin, gst_state_code, created_by)
VALUES (@legal_name, @display_name, @org_type, @is_buyer, @is_supplier, sqlc.narg(gstin), sqlc.narg(gst_state_code), sqlc.narg(created_by))
RETURNING *;

-- name: GetOrganization :one
SELECT * FROM organizations WHERE id = @id AND deleted_at IS NULL;

-- name: GrantRole :one
INSERT INTO user_roles (user_id, role_code, organization_id, granted_by)
VALUES (@user_id, @role_code, sqlc.narg(organization_id), sqlc.narg(granted_by))
RETURNING *;

-- name: RevokeRole :execrows
UPDATE user_roles SET revoked_at = now()
WHERE user_id = @user_id AND role_code = @role_code
  AND organization_id IS NOT DISTINCT FROM sqlc.narg(organization_id)
  AND revoked_at IS NULL;

-- name: ListUserRoles :many
SELECT ur.role_code, r.scope, ur.organization_id, o.display_name AS organization_name
FROM user_roles ur
JOIN roles r ON r.code = ur.role_code
LEFT JOIN organizations o ON o.id = ur.organization_id
WHERE ur.user_id = @user_id AND ur.revoked_at IS NULL
ORDER BY r.scope, ur.role_code;

-- name: ListUserPermissions :many
-- Effective permissions of a user acting in an organisation: org-scoped
-- roles for that organisation plus all platform roles.
SELECT DISTINCT rp.permission_code::text AS permission
FROM user_roles ur
JOIN role_permissions rp ON rp.role_code = ur.role_code
WHERE ur.user_id = @user_id
  AND ur.revoked_at IS NULL
  AND (ur.organization_id IS NULL OR ur.organization_id = sqlc.narg(organization_id))
ORDER BY permission;

-- name: UserHasPermission :one
SELECT EXISTS (
  SELECT 1 FROM user_roles ur
  JOIN role_permissions rp ON rp.role_code = ur.role_code
  WHERE ur.user_id = @user_id AND ur.revoked_at IS NULL
    AND rp.permission_code = @permission
    AND (ur.organization_id IS NULL OR ur.organization_id = sqlc.narg(organization_id))
) AS allowed;

-- name: CreateAddress :one
INSERT INTO addresses (organization_id, label, line1, city, state_code, pincode, location, is_site)
VALUES (@organization_id, @label, @line1, @city, @state_code, @pincode,
        ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, @is_site)
RETURNING id;

-- name: CreateOperatorProfile :exec
INSERT INTO operator_profiles (user_id, employer_organization_id, licence_class, licence_expires_on, experience_years, on_duty)
VALUES (@user_id, sqlc.narg(employer_organization_id), sqlc.narg(licence_class), sqlc.narg(licence_expires_on), sqlc.narg(experience_years), @on_duty);

-- name: CreateBankAccount :one
INSERT INTO bank_accounts (organization_id, account_holder_name, account_number_enc, account_number_hash, account_last4, ifsc, is_primary)
VALUES (@organization_id, @account_holder_name, @account_number_enc, @account_number_hash, @account_last4, @ifsc, @is_primary)
RETURNING id;

-- name: SetActorContext :exec
-- Identifies the actor for audit/history triggers for the rest of the transaction.
SELECT set_config('app.actor_user_id', @actor_user_id::text, true),
       set_config('app.actor_role', @actor_role::text, true),
       set_config('app.reason', @reason::text, true);
