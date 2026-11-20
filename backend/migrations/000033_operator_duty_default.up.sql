-- Root cause of "Off duty" drivers: on_duty defaulted to false and nothing ever turned it on, so a
-- freshly verified driver was ineligible for every assignment until they found the toggle.
-- on_duty is now the driver's own manual switch and defaults to on; availability (assigned / on job)
-- is derived from assignments and never overwrites the switch.
ALTER TABLE operator_profiles ALTER COLUMN on_duty SET DEFAULT true;
UPDATE operator_profiles SET on_duty = true WHERE verified_at IS NOT NULL AND NOT on_duty;
