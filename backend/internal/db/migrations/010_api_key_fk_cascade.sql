-- +migrate Up
-- When an API key is deleted, the three tables that reference it must not
-- block the delete. Historically these FKs were created without any ON
-- DELETE clause, which defaults to NO ACTION — so deleting a key that had
-- ever been used returned a 500 from DELETE /v1/management/keys/{id}.
--
-- We keep the usage rows (spend_logs, response_objects, daily_spend) for
-- accounting and audit, and just null out the link to the now-gone key.
-- Postgres treats NULLs in UNIQUE constraints as distinct, so the
-- daily_spend UNIQUE(api_key_id, team_id, date) stays valid.

ALTER TABLE spend_logs DROP CONSTRAINT IF EXISTS spend_logs_api_key_id_fkey;
ALTER TABLE spend_logs
    ADD CONSTRAINT spend_logs_api_key_id_fkey
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE SET NULL;

ALTER TABLE response_objects DROP CONSTRAINT IF EXISTS response_objects_api_key_id_fkey;
ALTER TABLE response_objects
    ADD CONSTRAINT response_objects_api_key_id_fkey
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE SET NULL;

ALTER TABLE daily_spend DROP CONSTRAINT IF EXISTS daily_spend_api_key_id_fkey;
ALTER TABLE daily_spend
    ADD CONSTRAINT daily_spend_api_key_id_fkey
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE SET NULL;

-- +migrate Down
ALTER TABLE spend_logs DROP CONSTRAINT IF EXISTS spend_logs_api_key_id_fkey;
ALTER TABLE spend_logs
    ADD CONSTRAINT spend_logs_api_key_id_fkey
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id);

ALTER TABLE response_objects DROP CONSTRAINT IF EXISTS response_objects_api_key_id_fkey;
ALTER TABLE response_objects
    ADD CONSTRAINT response_objects_api_key_id_fkey
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id);

ALTER TABLE daily_spend DROP CONSTRAINT IF EXISTS daily_spend_api_key_id_fkey;
ALTER TABLE daily_spend
    ADD CONSTRAINT daily_spend_api_key_id_fkey
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id);
