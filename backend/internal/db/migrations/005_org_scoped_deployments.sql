-- +migrate Up

ALTER TABLE deployments ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_deployments_org_id ON deployments(org_id);

-- +migrate Down

DROP INDEX IF EXISTS idx_deployments_org_id;
ALTER TABLE deployments DROP COLUMN IF EXISTS org_id;
