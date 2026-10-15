-- +goose Up
-- incident_digest_queue.digested_at: when the item was taken into a digest alert. A taken item is kept for a day, then
-- the housekeeping deletes it; NULL is an item still waiting.
ALTER TABLE `incident_digest_queue` ADD COLUMN `digested_at` datetime(3) DEFAULT NULL, ADD KEY `idx_incident_digest_queue_digested_at` (`digested_at`);
