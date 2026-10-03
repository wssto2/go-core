-- +goose Up
-- identity: one account per e-mail address. email becomes NULL-able (an account without an address is NULL, not an empty
-- string, so any number of them may exist), the empty strings P1 stored become NULL, and the address is unique. The
-- column's collation is case-insensitive on MySQL and MariaDB, so "Ana@x" and "ana@x" are one address; the store keeps it
-- lower-case anyway. This fails, naming the address, when two accounts already share one: fix those rows first.
ALTER TABLE accounts MODIFY email VARCHAR(255) NULL;
UPDATE accounts SET email = NULL WHERE email = '';
ALTER TABLE accounts ADD UNIQUE KEY uq_accounts_email (email);
