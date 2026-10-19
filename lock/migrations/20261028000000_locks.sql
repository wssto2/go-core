-- +goose Up
-- lock: named leases, one row per key. A holder owns a key until lease_until and keeps it by refreshing; a lease
-- that is not refreshed is free for anyone after that time. No foreign key; the owner is an opaque string.
--   lock_key     the name of the lease, for example 'vega-import'
--   owner        who holds it now (unique per holder, never shared by two runs)
--   lease_until  the instant after which anyone may take it
CREATE TABLE IF NOT EXISTS locks (
  lock_key VARCHAR(190) NOT NULL,
  owner VARCHAR(100) NOT NULL,
  lease_until DATETIME(3) NOT NULL,
  PRIMARY KEY (lock_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
