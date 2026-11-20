-- name: CreateMaintenancePlan :one
INSERT INTO maintenance_plans (equipment_id, task, every_hours, every_days, last_done_hmr, last_done_at)
VALUES (@equipment_id, @task, sqlc.narg(every_hours), sqlc.narg(every_days), sqlc.narg(last_done_hmr), sqlc.narg(last_done_at))
RETURNING *;

-- name: ListMaintenancePlans :many
SELECT * FROM maintenance_plans WHERE equipment_id = @equipment_id AND is_active ORDER BY created_at;

-- name: GetMaintenancePlan :one
SELECT * FROM maintenance_plans WHERE id = @id AND equipment_id = @equipment_id;

-- name: CreateMaintenanceRecord :one
INSERT INTO maintenance_records (equipment_id, plan_id, task, status, scheduled_for, performed_at, hmr_at, cost_paise, vendor, notes, recorded_by)
VALUES (@equipment_id, sqlc.narg(plan_id), @task, @status, sqlc.narg(scheduled_for), sqlc.narg(performed_at),
        sqlc.narg(hmr_at), sqlc.narg(cost_paise), sqlc.narg(vendor), sqlc.narg(notes), sqlc.narg(recorded_by))
RETURNING *;

-- name: ListMaintenanceRecords :many
SELECT * FROM maintenance_records WHERE equipment_id = @equipment_id ORDER BY created_at DESC LIMIT @result_limit;

-- name: GetMaintenanceRecord :one
SELECT * FROM maintenance_records WHERE id = @id AND equipment_id = @equipment_id;

-- name: UpdateMaintenanceRecordStatus :one
UPDATE maintenance_records
SET status       = @status,
    performed_at = CASE WHEN @status::maintenance_status = 'done' THEN COALESCE(sqlc.narg(performed_at), now()) ELSE NULL END,
    hmr_at       = COALESCE(sqlc.narg(hmr_at), hmr_at),
    cost_paise   = COALESCE(sqlc.narg(cost_paise), cost_paise),
    notes        = COALESCE(sqlc.narg(notes), notes)
WHERE id = @id AND equipment_id = @equipment_id AND status IN ('scheduled', 'in_progress')
RETURNING *;

-- name: MarkMaintenancePlanDone :exec
UPDATE maintenance_plans SET last_done_at = @done_at, last_done_hmr = COALESCE(sqlc.narg(hmr), last_done_hmr)
WHERE id = @id;

-- name: ListDueMaintenance :many
-- Active plans that are due by calendar or by hour-meter.
SELECT p.id AS plan_id, p.task, p.equipment_id, e.title, e.owner_org_id, e.current_hmr, p.every_hours, p.every_days,
       p.last_done_at, p.last_done_hmr
FROM maintenance_plans p
JOIN equipment e ON e.id = p.equipment_id AND e.deleted_at IS NULL
WHERE p.is_active
  AND (sqlc.narg(owner_org_id)::uuid IS NULL OR e.owner_org_id = sqlc.narg(owner_org_id))
  AND ((p.every_days IS NOT NULL AND (p.last_done_at IS NULL OR p.last_done_at + make_interval(days => p.every_days) <= now()))
    OR (p.every_hours IS NOT NULL AND e.current_hmr - COALESCE(p.last_done_hmr, 0) >= p.every_hours))
  AND NOT EXISTS (SELECT 1 FROM maintenance_records r WHERE r.plan_id = p.id AND r.status IN ('scheduled', 'in_progress'))
ORDER BY e.title, p.task;
