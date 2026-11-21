-- Catalogue changes by administrators are audited (actor and reason come from the transaction).
CREATE TRIGGER equipment_categories_audit AFTER INSERT OR UPDATE ON equipment_categories FOR EACH ROW EXECUTE FUNCTION audit_row_change();
