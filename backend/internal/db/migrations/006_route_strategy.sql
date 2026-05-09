-- +migrate Up

ALTER TABLE deployments ADD COLUMN IF NOT EXISTS routing_strategy TEXT NOT NULL DEFAULT 'round-robin';
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS weight INT NOT NULL DEFAULT 1;
CREATE INDEX IF NOT EXISTS idx_deployments_alias_org ON deployments(model_alias, org_id);

-- +migrate Down

DROP INDEX IF EXISTS idx_deployments_alias_org;
ALTER TABLE deployments DROP COLUMN IF EXISTS weight;
ALTER TABLE deployments DROP COLUMN IF EXISTS routing_strategy;
