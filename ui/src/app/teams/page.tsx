'use client';

import { useEffect, useState } from 'react';
import {
  getTeams,
  createTeam,
  deleteTeam,
  getOrganizations,
} from '@/lib/api';
import type { Organization, Team } from '@/lib/types';
import { PageHeader, Alert, LoadingSkeleton, EmptyState } from '@/components/ui';

export default function TeamsPage() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [showCreate, setShowCreate] = useState(false);

  const [name, setName] = useState('');
  const [orgId, setOrgId] = useState('');
  const [models, setModels] = useState('');
  const [rpmLimit, setRpmLimit] = useState('');
  const [tpmLimit, setTpmLimit] = useState('');
  const [maxBudget, setMaxBudget] = useState('');

  async function load() {
    try {
      const [teamData, orgData] = await Promise.all([getTeams(), getOrganizations()]);
      setTeams(teamData);
      setOrgs(orgData);
      setError('');
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to load teams');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  function resetForm() {
    setName('');
    setOrgId('');
    setModels('');
    setRpmLimit('');
    setTpmLimit('');
    setMaxBudget('');
    setShowCreate(false);
  }

  async function handleCreate() {
    try {
      setError('');
      const modelList = models
        .split(',')
        .map((m) => m.trim())
        .filter(Boolean);
      await createTeam({
        name,
        org_id: orgId || null,
        models: modelList.length > 0 ? modelList : undefined,
        rpm_limit: rpmLimit ? parseInt(rpmLimit, 10) : null,
        tpm_limit: tpmLimit ? parseInt(tpmLimit, 10) : null,
        max_budget: maxBudget ? parseFloat(maxBudget) : null,
      });
      setSuccess(`Team "${name}" created.`);
      resetForm();
      load();
      setTimeout(() => setSuccess(''), 4000);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Create failed');
    }
  }

  async function handleDelete(id: string, teamName: string) {
    if (!confirm(`Delete team "${teamName}"? All associated API keys will be deactivated.`))
      return;
    try {
      setError('');
      await deleteTeam(id);
      setSuccess(`Team "${teamName}" deleted.`);
      load();
      setTimeout(() => setSuccess(''), 4000);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Delete failed');
    }
  }

  function orgName(id: string | null | undefined): string {
    if (!id) return '—';
    const org = orgs.find((o) => o.id === id);
    return org ? org.name : id.slice(0, 8);
  }

  function budgetBar(spend: number, budget: number | null | undefined) {
    if (budget == null) return null;
    const pct = budget > 0 ? Math.min((spend / budget) * 100, 100) : 0;
    const color =
      pct >= 90 ? 'bg-red-500' : pct >= 70 ? 'bg-amber-500' : 'bg-emerald-500';
    return (
      <div className="mt-1.5 flex items-center gap-2">
        <div className="h-1.5 flex-1 rounded-full bg-zinc-800">
          <div
            className={`h-1.5 rounded-full transition-all ${color}`}
            style={{ width: `${pct}%` }}
          />
        </div>
        <span className="text-[11px] tabular-nums text-zinc-500">
          {pct.toFixed(0)}%
        </span>
      </div>
    );
  }

  if (loading) return <LoadingSkeleton rows={5} />;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Teams"
        description="Manage teams, assign organizations, and track budgets."
        action={
          <button onClick={() => setShowCreate(!showCreate)} className="gatewayllm-btn">
            {showCreate ? 'Cancel' : 'Create Team'}
          </button>
        }
      />

      {error && <Alert variant="error" onDismiss={() => setError('')}>{error}</Alert>}
      {success && <Alert variant="success" onDismiss={() => setSuccess('')}>{success}</Alert>}

      {showCreate && (
        <div className="gatewayllm-card p-5 space-y-4">
          <h2 className="text-lg font-medium text-white">New Team</h2>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Name *</label>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="gatewayllm-input"
                placeholder="Backend team"
              />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Organization</label>
              <select
                value={orgId}
                onChange={(e) => setOrgId(e.target.value)}
                className="gatewayllm-input"
              >
                <option value="">None</option>
                {orgs.map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.name}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Models (comma-separated)</label>
              <input
                value={models}
                onChange={(e) => setModels(e.target.value)}
                className="gatewayllm-input"
                placeholder="gpt-4o, claude-3"
              />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">RPM Limit</label>
              <input
                type="number"
                value={rpmLimit}
                onChange={(e) => setRpmLimit(e.target.value)}
                className="gatewayllm-input"
                placeholder="60"
              />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">TPM Limit</label>
              <input
                type="number"
                value={tpmLimit}
                onChange={(e) => setTpmLimit(e.target.value)}
                className="gatewayllm-input"
                placeholder="100000"
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
                placeholder="500.00"
              />
            </div>
          </div>
          <button
            onClick={handleCreate}
            disabled={!name}
            className="gatewayllm-btn disabled:opacity-50 disabled:cursor-not-allowed"
          >
            Create Team
          </button>
        </div>
      )}

      <div className="gatewayllm-table-wrap">
        <table className="gatewayllm-table">
          <thead>
            <tr>
              <th className="gatewayllm-th">Name</th>
              <th className="gatewayllm-th">Organization</th>
              <th className="gatewayllm-th">Models</th>
              <th className="gatewayllm-th text-right">Budget</th>
              <th className="gatewayllm-th text-right">Spend</th>
              <th className="gatewayllm-th text-right">RPM / TPM</th>
              <th className="gatewayllm-th">Created</th>
              <th className="gatewayllm-th text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {teams.length === 0 ? (
              <tr>
                <td colSpan={8}>
                  <EmptyState
                    message="No teams yet."
                    action={() => setShowCreate(true)}
                    actionLabel="Create your first team"
                  />
                </td>
              </tr>
            ) : (
              teams.map((t) => (
                <tr key={t.id} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                  <td className="px-4 py-3">
                    <span className="font-medium text-white">{t.name}</span>
                    {budgetBar(t.total_spend, t.max_budget)}
                  </td>
                  <td className="px-4 py-3 text-zinc-400">{orgName(t.org_id)}</td>
                  <td className="px-4 py-3">
                    {t.models && t.models.length > 0 ? (
                      <div className="flex flex-wrap gap-1">
                        {t.models.map((m) => (
                          <span
                            key={m}
                            className="rounded bg-zinc-800 px-1.5 py-0.5 text-xs text-zinc-300"
                          >
                            {m}
                          </span>
                        ))}
                      </div>
                    ) : (
                      <span className="text-zinc-600">All</span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-right tabular-nums text-zinc-300">
                    {t.max_budget != null ? `$${t.max_budget.toFixed(2)}` : '—'}
                  </td>
                  <td className="px-4 py-3 text-right tabular-nums text-zinc-300">
                    ${t.total_spend.toFixed(4)}
                  </td>
                  <td className="px-4 py-3 text-right tabular-nums text-zinc-400">
                    {t.rpm_limit ?? '—'} / {t.tpm_limit ?? '—'}
                  </td>
                  <td className="px-4 py-3 text-zinc-400">
                    {new Date(t.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <button
                      onClick={() => handleDelete(t.id, t.name)}
                      className="text-xs text-red-400 hover:text-red-300"
                    >
                      Delete
                    </button>
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
