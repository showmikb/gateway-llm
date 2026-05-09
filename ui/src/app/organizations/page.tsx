'use client';

import { useEffect, useState } from 'react';
import {
  getOrganizations,
  createOrganization,
  updateOrganization,
  deleteOrganization,
} from '@/lib/api';
import type { Organization } from '@/lib/types';
import { PageHeader, Alert, LoadingSkeleton, EmptyState, Badge } from '@/components/ui';
import { ContactTeamModal } from '@/components/ContactTeamModal';
import { useAuth } from '@/contexts/AuthContext';

export default function OrganizationsPage() {
  const { isAdmin } = useAuth();
  // Creating/deleting organizations is a super/org admin action; team admins
  // and members see an overlay instead.
  const canManageOrgs = isAdmin;

  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [showContactModal, setShowContactModal] = useState(false);

  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [maxBudget, setMaxBudget] = useState('');

  async function load() {
    try {
      const data = await getOrganizations();
      setOrgs(data);
      setError('');
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function handleCreate() {
    try {
      await createOrganization({
        name,
        slug: slug || undefined,
        max_budget: maxBudget ? parseFloat(maxBudget) : undefined,
      });
      setName('');
      setSlug('');
      setMaxBudget('');
      setShowCreate(false);
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Create failed');
    }
  }

  async function handleDelete(id: string) {
    if (!confirm('Delete this organization? This cannot be undone.')) return;
    try {
      await deleteOrganization(id);
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Delete failed');
    }
  }

  async function handleToggle(org: Organization) {
    try {
      await updateOrganization(org.id, { is_active: !org.is_active });
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Update failed');
    }
  }

  if (loading) return <LoadingSkeleton rows={4} />;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Organizations"
        description={
          canManageOrgs
            ? 'Manage organizations and their budgets.'
            : 'View organizations. Only admins can create or modify them.'
        }
        action={
          <button
            onClick={() => {
              if (!canManageOrgs) {
                setShowContactModal(true);
                return;
              }
              setShowCreate(!showCreate);
            }}
            className="gatewayllm-btn"
          >
            {showCreate && canManageOrgs ? 'Cancel' : 'Create Organization'}
          </button>
        }
      />

      <ContactTeamModal
        open={showContactModal}
        onClose={() => setShowContactModal(false)}
        title="Organization management is admin-only"
        message="Creating, activating, or deleting organizations requires an admin. Ask an admin on your team to make the change, or contact us."
      />

      {error && <Alert variant="error" onDismiss={() => setError('')}>{error}</Alert>}

      {showCreate && canManageOrgs && (
        <div className="gatewayllm-card p-5 space-y-4">
          <h2 className="text-lg font-medium text-white">New Organization</h2>
          <div className="grid grid-cols-3 gap-4">
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Name</label>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="gatewayllm-input"
                placeholder="Acme Corp"
              />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Slug (optional)</label>
              <input
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
                className="gatewayllm-input"
                placeholder="acme-corp"
              />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Max Budget (USD)</label>
              <input
                type="number"
                step="0.01"
                value={maxBudget}
                onChange={(e) => setMaxBudget(e.target.value)}
                className="gatewayllm-input"
                placeholder="10000.00"
              />
            </div>
          </div>
          <button
            onClick={handleCreate}
            disabled={!name}
            className="gatewayllm-btn disabled:opacity-50 disabled:cursor-not-allowed"
          >
            Create
          </button>
        </div>
      )}

      <div className="gatewayllm-table-wrap">
        <table className="gatewayllm-table">
          <thead>
            <tr>
              <th className="gatewayllm-th">Name</th>
              <th className="gatewayllm-th">Slug</th>
              <th className="gatewayllm-th text-right">Budget</th>
              <th className="gatewayllm-th text-right">Spend</th>
              <th className="gatewayllm-th">Status</th>
              <th className="gatewayllm-th">Created</th>
              <th className="gatewayllm-th text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {orgs.length === 0 ? (
              <tr>
                <td colSpan={7}>
                  <EmptyState
                    message="No organizations yet."
                    action={canManageOrgs ? () => setShowCreate(true) : () => setShowContactModal(true)}
                    actionLabel={canManageOrgs ? 'Create your first organization' : 'Contact the team'}
                  />
                </td>
              </tr>
            ) : (
              orgs.map((o) => (
                <tr key={o.id} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                  <td className="px-4 py-3 font-medium text-white">{o.name}</td>
                  <td className="px-4 py-3 font-mono text-sm text-zinc-400">{o.slug}</td>
                  <td className="px-4 py-3 text-right tabular-nums text-zinc-300">
                    {o.max_budget != null ? `$${o.max_budget.toFixed(2)}` : '—'}
                  </td>
                  <td className="px-4 py-3 text-right tabular-nums text-zinc-300">
                    ${o.total_spend.toFixed(2)}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={o.is_active ? 'success' : 'danger'}>
                      {o.is_active ? 'Active' : 'Inactive'}
                    </Badge>
                  </td>
                  <td className="px-4 py-3 text-zinc-400">
                    {new Date(o.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3 text-right space-x-2">
                    {canManageOrgs ? (
                      <>
                        <button
                          onClick={() => handleToggle(o)}
                          className={`text-xs ${o.is_active ? 'text-amber-400 hover:text-amber-300' : 'text-emerald-400 hover:text-emerald-300'}`}
                        >
                          {o.is_active ? 'Deactivate' : 'Activate'}
                        </button>
                        <button
                          onClick={() => handleDelete(o.id)}
                          className="text-xs text-red-400 hover:text-red-300"
                        >
                          Delete
                        </button>
                      </>
                    ) : (
                      <button
                        type="button"
                        onClick={() => setShowContactModal(true)}
                        className="text-xs text-zinc-400 hover:text-zinc-200"
                      >
                        Request change
                      </button>
                    )}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
