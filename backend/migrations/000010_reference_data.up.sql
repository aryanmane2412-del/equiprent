-- Reference data required in every environment. Development fixtures live in
-- backend/seeds/dev_seed.sql and are never applied by migrations.

INSERT INTO roles (code, name, scope, description) VALUES
  ('customer',      'Customer',          'organization', 'Books and pays for equipment'),
  ('owner',         'Equipment owner',   'organization', 'Lists equipment, accepts bookings, receives payouts'),
  ('fleet_manager', 'Fleet manager',     'organization', 'Runs an owner organisation''s fleet and operators'),
  ('operator',      'Operator / driver', 'organization', 'Operates machines on assigned jobs'),
  ('org_admin',     'Organisation admin','organization', 'Manages members and business details'),
  ('admin_super',   'Super admin',       'platform',     'Full platform access'),
  ('admin_ops',     'Operations admin',  'platform',     'Bookings, listings, disputes'),
  ('admin_kyc',     'KYC reviewer',      'platform',     'Verifies identity and documents'),
  ('admin_finance', 'Finance admin',     'platform',     'Refunds, payouts, reconciliation'),
  ('admin_support', 'Support agent',     'platform',     'Support tickets and account help');

INSERT INTO permissions (code, description) VALUES
  ('booking.create',          'Request or book equipment'),
  ('booking.view',            'View bookings of own organisation'),
  ('booking.manage',          'Accept, reject and counter booking requests'),
  ('booking.assign_operator', 'Assign operators to bookings'),
  ('booking.override',        'Force booking transitions (audited)'),
  ('equipment.create',        'Create equipment listings'),
  ('equipment.update',        'Edit equipment, availability and documents'),
  ('equipment.moderate',      'Approve or suspend listings'),
  ('pricing.manage',          'Set prices and mobilisation rates'),
  ('payment.create',          'Pay for bookings'),
  ('payment.refund',          'Issue refunds'),
  ('payout.view',             'View settlements and payouts'),
  ('payout.release',          'Release or hold payouts'),
  ('kyc.submit',              'Submit KYC documents'),
  ('kyc.review',              'Approve or reject KYC'),
  ('member.manage',           'Invite and remove organisation members'),
  ('user.manage',             'Suspend and reactivate platform users'),
  ('job.view',                'View assigned jobs'),
  ('job.update',              'Submit checklists and hour logs'),
  ('location.share',          'Share live location during active jobs'),
  ('tracking.view',           'View live location of own bookings or fleet'),
  ('maintenance.manage',      'Plan and record maintenance'),
  ('dispute.raise',           'Raise disputes on own bookings'),
  ('dispute.resolve',         'Resolve disputes (audited)'),
  ('review.create',           'Review counterparties after a booking'),
  ('report.fleet',            'Fleet and earnings reports'),
  ('report.platform',         'Platform-wide analytics and finance reports'),
  ('support.handle',          'Work support tickets'),
  ('config.manage',           'Commission rules, policies, categories, cities'),
  ('audit.view',              'Read the audit log');

INSERT INTO role_permissions (role_code, permission_code) VALUES
  ('customer', 'booking.create'), ('customer', 'booking.view'), ('customer', 'payment.create'),
  ('customer', 'tracking.view'), ('customer', 'dispute.raise'), ('customer', 'review.create'), ('customer', 'kyc.submit'),

  ('owner', 'booking.view'), ('owner', 'booking.manage'), ('owner', 'booking.assign_operator'),
  ('owner', 'equipment.create'), ('owner', 'equipment.update'), ('owner', 'pricing.manage'),
  ('owner', 'payout.view'), ('owner', 'kyc.submit'), ('owner', 'member.manage'), ('owner', 'tracking.view'),
  ('owner', 'maintenance.manage'), ('owner', 'dispute.raise'), ('owner', 'review.create'), ('owner', 'report.fleet'),

  ('fleet_manager', 'booking.view'), ('fleet_manager', 'booking.assign_operator'), ('fleet_manager', 'equipment.update'),
  ('fleet_manager', 'tracking.view'), ('fleet_manager', 'maintenance.manage'), ('fleet_manager', 'report.fleet'),

  ('operator', 'job.view'), ('operator', 'job.update'), ('operator', 'location.share'),

  ('org_admin', 'member.manage'), ('org_admin', 'kyc.submit'), ('org_admin', 'booking.view'), ('org_admin', 'payout.view'),

  ('admin_ops', 'booking.view'), ('admin_ops', 'booking.override'), ('admin_ops', 'equipment.moderate'),
  ('admin_ops', 'dispute.resolve'), ('admin_ops', 'tracking.view'), ('admin_ops', 'user.manage'), ('admin_ops', 'report.platform'),
  ('admin_kyc', 'kyc.review'), ('admin_kyc', 'equipment.moderate'),
  ('admin_finance', 'payment.refund'), ('admin_finance', 'payout.release'), ('admin_finance', 'payout.view'),
  ('admin_finance', 'dispute.resolve'), ('admin_finance', 'report.platform'),
  ('admin_support', 'support.handle'), ('admin_support', 'user.manage'), ('admin_support', 'booking.view');

-- Super admin holds every permission.
INSERT INTO role_permissions (role_code, permission_code)
SELECT 'admin_super', code FROM permissions;

INSERT INTO equipment_categories (slug, name, sort_order, spec_schema) VALUES
  ('backhoe-loader', '{"en": "Backhoe Loaders", "hi": "बैकहो लोडर"}', 10, '{"operating_weight_kg": "number", "bucket_m3": "number", "dig_depth_m": "number", "engine_hp": "number"}'),
  ('excavator',      '{"en": "Excavators", "hi": "एक्सकेवेटर"}', 20, '{"operating_weight_kg": "number", "bucket_m3": "number", "dig_depth_m": "number", "reach_m": "number"}'),
  ('crane',          '{"en": "Cranes", "hi": "क्रेन"}', 30, '{"max_capacity_t": "number", "boom_length_m": "number", "lift_height_m": "number"}'),
  ('roller',         '{"en": "Rollers & Compactors", "hi": "रोलर"}', 40, '{"operating_weight_kg": "number", "drum_width_mm": "number"}'),
  ('transit-mixer',  '{"en": "Transit Mixers", "hi": "ट्रांजिट मिक्सर"}', 50, '{"drum_capacity_m3": "number"}'),
  ('tipper',         '{"en": "Tippers & Dumpers", "hi": "टिपर"}', 60, '{"payload_t": "number", "body_volume_m3": "number"}'),
  ('telehandler',    '{"en": "Telehandlers", "hi": "टेलीहैंडलर"}', 70, '{"max_capacity_kg": "number", "lift_height_m": "number"}'),
  ('dozer-grader',   '{"en": "Dozers & Graders", "hi": "डोज़र"}', 80, '{"operating_weight_kg": "number", "blade_width_m": "number"}');

INSERT INTO cancellation_policies (code, name, rules, is_default) VALUES
  ('standard', 'Standard — free until 24 hours before start',
   '[{"hours_before": 24, "fee_bps": 0}, {"hours_before": 0, "fee_bps": 2500}]', true),
  ('strict', 'Strict — 50% of the first day within 72 hours',
   '[{"hours_before": 72, "fee_bps": 0}, {"hours_before": 0, "fee_bps": 5000}]', false);

-- GST state codes: 27 Maharashtra, 36 Telangana, 29 Karnataka, 24 Gujarat.
INSERT INTO cities (name, state_code, center, is_live) VALUES
  ('Pune',      '27', ST_SetSRID(ST_MakePoint(73.8567, 18.5204), 4326)::geography, true),
  ('Mumbai',    '27', ST_SetSRID(ST_MakePoint(72.8777, 19.0760), 4326)::geography, false),
  ('Hyderabad', '36', ST_SetSRID(ST_MakePoint(78.4867, 17.3850), 4326)::geography, false),
  ('Bengaluru', '29', ST_SetSRID(ST_MakePoint(77.5946, 12.9716), 4326)::geography, false),
  ('Ahmedabad', '24', ST_SetSRID(ST_MakePoint(72.5714, 23.0225), 4326)::geography, false);

-- Platform-wide ledger accounts (per-owner payable accounts are created on demand).
INSERT INTO ledger_accounts (code, type) VALUES
  ('gateway_clearing',                'asset'),
  ('customer_deposits',               'liability'),
  ('platform_revenue:commission',     'revenue'),
  ('platform_revenue:convenience_fee','revenue'),
  ('gst_output',                      'liability'),
  ('tcs_payable',                     'liability'),
  ('tds_194o_payable',                'liability'),
  ('gateway_fees',                    'expense');

-- Default commission: 10% owner commission + 2% customer fee.
INSERT INTO commission_rules (commission_bps, customer_fee_bps, priority) VALUES (1000, 200, 0);
