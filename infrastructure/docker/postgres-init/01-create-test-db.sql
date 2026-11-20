-- Runs once, when the postgres volume is first created.
-- A separate database for integration tests (scripts/test.ps1 -Integration),
-- so tests never touch development data.
CREATE DATABASE equiprent_test;
