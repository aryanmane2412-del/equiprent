-- name: UpsertOperatorProfile :one
INSERT INTO operator_profiles (user_id, employer_organization_id, licence_class, licence_expires_on, licence_last4, experience_years)
VALUES (@user_id, sqlc.narg(employer_organization_id), sqlc.narg(licence_class), sqlc.narg(licence_expires_on),
        sqlc.narg(licence_last4), sqlc.narg(experience_years))
ON CONFLICT (user_id) DO UPDATE
SET employer_organization_id = COALESCE(EXCLUDED.employer_organization_id, operator_profiles.employer_organization_id),
    licence_class            = EXCLUDED.licence_class,
    licence_expires_on       = EXCLUDED.licence_expires_on,
    licence_last4            = EXCLUDED.licence_last4,
    experience_years         = EXCLUDED.experience_years
RETURNING *;

-- name: GetOperatorProfile :one
SELECT * FROM operator_profiles WHERE user_id = @user_id;

-- name: SetOperatorDuty :one
UPDATE operator_profiles SET on_duty = @on_duty WHERE user_id = @user_id
RETURNING on_duty;

-- name: DeleteOperatorSkills :exec
DELETE FROM operator_skills WHERE operator_user_id = @operator_user_id;

-- name: AddOperatorSkills :exec
INSERT INTO operator_skills (operator_user_id, category_id)
SELECT @operator_user_id, c.id FROM equipment_categories c WHERE c.slug = ANY(@category_slugs::text[])
ON CONFLICT DO NOTHING;

-- name: ListOperatorSkills :many
SELECT c.slug FROM operator_skills s JOIN equipment_categories c ON c.id = s.category_id
WHERE s.operator_user_id = @operator_user_id ORDER BY c.slug;

-- name: ListOperatorJobs :many
SELECT a.id AS assignment_id, a.status AS assignment_status, b.id AS booking_id, b.reference, b.status AS booking_status,
       lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at,
       e.title AS equipment_title, ad.line1 AS site_line1, ad.city AS site_city,
       ST_Y(b.site_location::geometry)::float8 AS site_lat, ST_X(b.site_location::geometry)::float8 AS site_lng
FROM operator_assignments a
JOIN bookings b ON b.id = a.booking_id
JOIN equipment e ON e.id = b.equipment_id
JOIN addresses ad ON ad.id = b.site_address_id
WHERE a.operator_user_id = @operator_user_id
  AND (sqlc.narg(status)::text IS NULL OR a.status::text = sqlc.narg(status))
ORDER BY lower(b.period) DESC
LIMIT @result_limit;

-- name: GetOperatorAssignment :one
SELECT a.id, a.status, a.booking_id, b.status AS booking_status, b.version AS booking_version
FROM operator_assignments a
JOIN bookings b ON b.id = a.booking_id
WHERE a.booking_id = @booking_id AND a.operator_user_id = @operator_user_id
  AND a.status IN ('assigned', 'accepted');

-- name: IsOrgOperator :one
SELECT EXISTS (
  SELECT 1 FROM user_roles ur
  JOIN operator_profiles op ON op.user_id = ur.user_id
  WHERE ur.user_id = @user_id AND ur.organization_id = @organization_id
    AND ur.role_code = 'operator' AND ur.revoked_at IS NULL
) AS ok;

-- name: OperatorVerificationLocked :one
SELECT EXISTS (
  SELECT 1 FROM verification_applications
  WHERE role = 'operator' AND user_id = @user_id AND status IN ('submitted', 'under_review', 'approved')
) AS locked;

-- name: InsertOperatorProfileIfAbsent :exec
-- Creates an empty profile for a new operator; an existing profile is never touched.
INSERT INTO operator_profiles (user_id, employer_organization_id)
VALUES (@user_id, @employer_organization_id)
ON CONFLICT (user_id) DO NOTHING;

-- name: SetOperatorEmployer :execrows
-- Moves only the employer. Licence, experience, duty and verification stay as they are.
UPDATE operator_profiles SET employer_organization_id = @employer_organization_id WHERE user_id = @user_id;

-- name: OperatorActiveElsewhere :one
-- The operator still holds the operator role in another organisation.
SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id = @user_id AND role_code = 'operator' AND revoked_at IS NULL
               AND organization_id IS DISTINCT FROM @organization_id::uuid)::bool AS active;
