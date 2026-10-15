-- +goose Up
-- incidents.source: the browser errors the application reports (POST /v1/incidents/client-errors) are an incident
-- source of their own. An incident seen from several sources is still 'mixed'.
ALTER TABLE `incidents` MODIFY COLUMN `source` enum('user_report','runtime_error','client_error','mixed') COLLATE utf8mb3_unicode_ci NOT NULL DEFAULT 'runtime_error';
