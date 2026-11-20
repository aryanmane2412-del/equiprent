ALTER TABLE operator_profiles DROP CONSTRAINT operator_home_address_complete_ck;
ALTER TABLE operator_profiles ADD CONSTRAINT operator_home_address_complete_ck CHECK (
  (home_address_line1 IS NULL AND home_address_city IS NULL AND home_address_state_code IS NULL AND home_address_pincode IS NULL)
  OR (home_address_line1 IS NOT NULL AND home_address_city IS NOT NULL AND home_address_state_code ~ '^[A-Z]{2}$' AND home_address_pincode ~ '^[0-9]{6}$')
);
