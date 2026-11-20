-- Keep a durable audit trail for maintenance status, costs, downtime and
-- inspection evidence. Checklists are immutable, but inserts still need audit.
CREATE TRIGGER maintenance_records_audit
  AFTER INSERT OR UPDATE OR DELETE ON maintenance_records
  FOR EACH ROW EXECUTE FUNCTION audit_row_change();

CREATE TRIGGER checklists_audit
  AFTER INSERT ON checklists
  FOR EACH ROW EXECUTE FUNCTION audit_row_change();
