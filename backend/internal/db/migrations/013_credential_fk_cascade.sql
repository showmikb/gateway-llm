-- +migrate Up
-- The deployments.credential_id FK was originally created without an
-- ON DELETE clause, which defaults to NO ACTION. As a result, deleting
-- a provider credential that has any deployment pointing at it raised
-- a Postgres FK violation (SQLSTATE 23503) and the UI delete button
-- appeared to silently fail.
--
-- We change the behavior to SET NULL: deleting a credential is now
-- always allowed, and any deployments still referencing it lose their
-- key link and stop routing until the operator picks a new credential.

ALTER TABLE deployments DROP CONSTRAINT IF EXISTS deployments_credential_id_fkey;
ALTER TABLE deployments
    ADD CONSTRAINT deployments_credential_id_fkey
    FOREIGN KEY (credential_id) REFERENCES provider_credentials(id) ON DELETE SET NULL;

-- +migrate Down
ALTER TABLE deployments DROP CONSTRAINT IF EXISTS deployments_credential_id_fkey;
ALTER TABLE deployments
    ADD CONSTRAINT deployments_credential_id_fkey
    FOREIGN KEY (credential_id) REFERENCES provider_credentials(id);
