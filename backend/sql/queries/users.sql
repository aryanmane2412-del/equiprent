-- name: UpdateUserProfile :one
UPDATE users
SET full_name          = COALESCE(sqlc.narg(full_name), full_name),
    email              = COALESCE(sqlc.narg(email)::citext, email),
    preferred_language = COALESCE(sqlc.narg(preferred_language), preferred_language)
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: InsertAddress :one
INSERT INTO addresses (organization_id, label, line1, line2, locality, city, district, state_code, pincode, location, is_site)
VALUES (@organization_id, @label, @line1, sqlc.narg(line2), sqlc.narg(locality), @city, sqlc.narg(district), @state_code, @pincode,
        CASE WHEN sqlc.narg(lat)::float8 IS NULL THEN NULL
             ELSE ST_SetSRID(ST_MakePoint(sqlc.narg(lng)::float8, sqlc.narg(lat)::float8), 4326)::geography END,
        @is_site)
RETURNING id, created_at;

-- name: GetAddress :one
SELECT id, organization_id, label, line1, line2, locality, city, district, state_code, pincode, is_site, created_at,
       COALESCE(ST_Y(location::geometry), 0)::float8 AS lat, COALESCE(ST_X(location::geometry), 0)::float8 AS lng, (location IS NOT NULL)::bool AS has_location
FROM addresses
WHERE id = @id AND deleted_at IS NULL;

-- name: ListOrgAddresses :many
SELECT id, organization_id, label, line1, line2, locality, city, district, state_code, pincode, is_site, created_at,
       COALESCE(ST_Y(location::geometry), 0)::float8 AS lat, COALESCE(ST_X(location::geometry), 0)::float8 AS lng, (location IS NOT NULL)::bool AS has_location
FROM addresses
WHERE organization_id = @organization_id AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: DeleteAddress :execrows
UPDATE addresses SET deleted_at = now()
WHERE id = @id AND organization_id = @organization_id AND deleted_at IS NULL;

-- name: UpdateOrganization :one
UPDATE organizations
SET display_name   = COALESCE(sqlc.narg(display_name), display_name),
    legal_name     = COALESCE(sqlc.narg(legal_name), legal_name),
    gstin          = COALESCE(sqlc.narg(gstin), gstin),
    gst_state_code = COALESCE(sqlc.narg(gst_state_code), gst_state_code)
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: ListOrgMembers :many
SELECT u.id AS user_id, u.full_name, u.phone, ur.role_code, ur.granted_at
FROM user_roles ur
JOIN users u ON u.id = ur.user_id AND u.deleted_at IS NULL
WHERE ur.organization_id = @organization_id AND ur.revoked_at IS NULL
ORDER BY u.full_name, ur.role_code;

-- name: ListOrgUserIDsWithRoles :many
-- Recipients for organisation-level notifications.
SELECT DISTINCT ur.user_id
FROM user_roles ur
JOIN users u ON u.id = ur.user_id AND u.status = 'active'
WHERE ur.organization_id = @organization_id AND ur.revoked_at IS NULL
  AND ur.role_code = ANY(@roles::text[]);

-- name: UserHasOrgRole :one
SELECT EXISTS (
  SELECT 1 FROM user_roles
  WHERE user_id = @user_id AND organization_id = @organization_id AND revoked_at IS NULL
    AND role_code = ANY(@roles::text[])
) AS ok;

-- name: InsertInvitation :one
INSERT INTO org_invitations (organization_id, invitee_user_id, role_code, invited_by)
VALUES (@organization_id, @invitee_user_id, @role_code, @invited_by)
ON CONFLICT (organization_id, invitee_user_id, role_code) WHERE status = 'pending' DO NOTHING
RETURNING *;

-- name: ListMyInvitations :many
SELECT i.id, i.organization_id, o.display_name AS organization_name, i.role_code, i.created_at, i.expires_at
FROM org_invitations i JOIN organizations o ON o.id = i.organization_id
WHERE i.invitee_user_id = @user_id AND i.status = 'pending' AND i.expires_at > now()
ORDER BY i.created_at DESC;

-- name: GetInvitationForUpdate :one
SELECT * FROM org_invitations WHERE id = @id AND invitee_user_id = @user_id FOR UPDATE;

-- name: RespondInvitation :execrows
UPDATE org_invitations SET status = @status, responded_at = now()
WHERE id = @id AND status = 'pending' AND (@status::text <> 'accepted' OR expires_at > now());

-- name: RevokeInvitation :execrows
UPDATE org_invitations SET status = 'revoked', responded_at = now()
WHERE id = @id AND organization_id = @organization_id AND status = 'pending';

-- name: ListOrgInvitations :many
SELECT i.id, i.invitee_user_id, u.full_name, u.phone, i.role_code, i.status, i.created_at, i.expires_at
FROM org_invitations i JOIN users u ON u.id = i.invitee_user_id
WHERE i.organization_id = @organization_id AND i.status = 'pending' AND i.expires_at > now()
ORDER BY i.created_at DESC;
