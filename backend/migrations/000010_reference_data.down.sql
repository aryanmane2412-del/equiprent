DELETE FROM commission_rules WHERE category_id IS NULL AND city_id IS NULL AND owner_org_id IS NULL AND priority = 0;
DELETE FROM ledger_accounts WHERE org_id IS NULL AND code IN (
  'gateway_clearing', 'customer_deposits', 'platform_revenue:commission', 'platform_revenue:convenience_fee',
  'gst_output', 'tcs_payable', 'tds_194o_payable', 'gateway_fees');
DELETE FROM cities WHERE name IN ('Pune', 'Mumbai', 'Hyderabad', 'Bengaluru', 'Ahmedabad');
DELETE FROM cancellation_policies WHERE code IN ('standard', 'strict');
DELETE FROM equipment_categories WHERE slug IN ('backhoe-loader', 'excavator', 'crane', 'roller', 'transit-mixer', 'tipper', 'telehandler', 'dozer-grader');
DELETE FROM role_permissions;
DELETE FROM permissions;
DELETE FROM roles;
