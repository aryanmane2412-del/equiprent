-- PostgreSQL cannot drop enum values. They stay; 000019's down migration
-- stops using them and the up migration re-adds them with IF NOT EXISTS.
-- Rolling back past 000006 drops the whole type.
SELECT 1;
