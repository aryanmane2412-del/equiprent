-- name: GetDatabaseInfo :one
-- Database name, server version and current time; used for start-up logs and smoke tests.
SELECT
  current_database()::text AS database_name,
  current_setting('server_version')::text AS server_version,
  now()::timestamptz AS server_time;
