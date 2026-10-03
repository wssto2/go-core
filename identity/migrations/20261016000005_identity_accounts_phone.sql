-- +goose Up
-- identity: an account's phone number, optional free text (the profile keeps it). A column on accounts,
-- after the e-mail address, empty for an account that never gave one.
ALTER TABLE accounts ADD COLUMN phone VARCHAR(30) NOT NULL DEFAULT '' AFTER email;
