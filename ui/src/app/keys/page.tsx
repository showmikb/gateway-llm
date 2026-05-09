'use client';

import Link from 'next/link';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { createKey, deleteKey, getDeployments, getKeys, getTeams, getUsers, updateKey } from '@/lib/api';
import { isUserApiKey, type APIKey, type DeploymentRow, type Team, type User } from '@/lib/types';
import { PageHeader, Alert, EmptyState, Badge, CopyButton } from '@/components/ui';
import { FirstVisitTip } from '@/components/FirstVisitTip';
import { KeyCreatedModal } from '@/components/KeyCreatedModal';
import { useAuth } from '@/contexts/AuthContext';
import { notifySetupChanged } from '@/lib/setupState';

export default function KeysPage() {
  const { isTeamAdmin, isViewer } = useAuth();
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [teams, setTeams] = useState<Team[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [deployments, setDeployments] = useState<DeploymentRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [revealed, setRevealed] = useState<string | null>(null);
  const [showKeyModal, setShowKeyModal] = useState(false);
  const [showForm, setShowForm] = useState(false);

  const [name, setName] = useState('');
  const [teamId, setTeamId] = useState('');
  const [userId, setUserId] = useState('');
  const [modelsAllowAll, setModelsAllowAll] = useState(true);
  const [modelSelections, setModelSelections] = useState<string[]>([]);
  const [rpmLimit, setRpmLimit] = useState('');
  const [tpmLimit, setTpmLimit] = useState('');
  const [maxBudget, setMaxBudget] = useState('');

  const [editKey, setEditKey] = useState<APIKey | null>(null);
  const [editName, setEditName] = useState('');
  const [editTeamId, setEditTeamId] = useState('');
  const [editUserId, setEditUserId] = useState('');
  const [editModelsAllowAll, setEditModelsAllowAll] = useState(true);
  const [editModelSelections, setEditModelSelections] = useState<string[]>([]);
  const [editRpmLimit, setEditRpmLimit] = useState('');
  const [editTpmLimit, setEditTpmLimit] = useState('');
  const [editMaxBudget, setEditMaxBudget] = useState('');
  const [editActive, setEditActive] = useState(true);
  const [saving, setSaving] = useState(false);

  const canWrite = isTeamAdmin && !isViewer;

  const uniqueAliases = useMemo(() => {
    const s = new Set<string>();
    for (const d of deployments) s.add(d.model_alias);
    return Array.from(s).sort();
  }, [deployments]);

  function toggleAlias(alias: string, set: string[], setFn: (v: string[]) => void) {
    if (set.includes(alias)) {
      setFn(set.filter((a) => a !== alias));
    } else {
      setFn([...set, alias]);
    }
  }

  const teamById = useMemo(() => {
    const m = new Map<string, string>();
    for (const t of teams) m.set(t.id, t.name);
    return m;
  }, [teams]);

  const userById = useMemo(() => {
    const m = new Map<string, string>();
    for (const u of users) m.set(u.id, u.email);
    return m;
  }, [users]);

  const load = useCallback(async () => {
    setError(null);
    const [k, deps] = await Promise.all([
      getKeys(),
      getDeployments().catch(() => [] as DeploymentRow[]),
    ]);
    setKeys(k.filter(isUserApiKey));
    setDeployments(deps);
    if (isTeamAdmin) {
      try {
        const [t, u] = await Promise.all([getTeams(), getUsers()]);
        setTeams(t);
        setUsers(u);
      } catch {
        // members don't have access to teams/users endpoints
      }
    }
  }, [isTeamAdmin]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        setLoading(true);
        await load();
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load keys');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [load]);

  async function onCreate() {
    const n = name.trim();
    if (!n) return;
    setCreating(true);
    setError(null);
    try {
      const body: Parameters<typeof createKey>[0] = { name: n };
      if (teamId) body.team_id = teamId;
      if (userId) body.user_id = userId;
      if (modelsAllowAll) {
        body.models = ['*'];
      } else if (modelSelections.length > 0) {
        body.models = modelSelections;
      }
      if (rpmLimit) body.rpm_limit = parseInt(rpmLimit);
      if (tpmLimit) body.tpm_limit = parseInt(tpmLimit);
      if (maxBudget) body.max_budget = parseFloat(maxBudget);
      const res = await createKey(body);
      setRevealed(res.key);
      if (typeof window !== 'undefined') {
        const seen = localStorage.getItem('gatewayllm_seen_key_try');
        if (!seen) {
          setShowKeyModal(true);
          localStorage.setItem('gatewayllm_seen_key_try', 'true');
        }
      }
      setName('');
      setTeamId('');
      setUserId('');
      setModelsAllowAll(true);
      setModelSelections([]);
      setRpmLimit('');
      setTpmLimit('');
      setMaxBudget('');
      setShowForm(false);
      notifySetupChanged();
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Create failed');
    } finally {
      setCreating(false);
    }
  }

  async function onDelete(id: string) {
    if (!confirm('Delete this API key? This cannot be undone.')) return;
    setError(null);
    try {
      await deleteKey(id);
      notifySetupChanged();
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Delete failed');
    }
  }

  async function onToggle(k: APIKey) {
    setError(null);
    try {
      await updateKey(k.id, { is_active: !k.is_active });
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Update failed');
    }
  }

  function openEdit(k: APIKey) {
    setEditKey(k);
    setEditName(k.name);
    setEditTeamId(k.team_id ?? '');
    setEditUserId(k.user_id ?? '');
    const m = k.models ?? [];
    const hasWildcard = m.includes('*');
    setEditModelsAllowAll(hasWildcard || m.length === 0);
    setEditModelSelections(hasWildcard ? [] : m);
    setEditRpmLimit(k.rpm_limit != null ? String(k.rpm_limit) : '');
    setEditTpmLimit(k.tpm_limit != null ? String(k.tpm_limit) : '');
    setEditMaxBudget(k.max_budget != null ? String(k.max_budget) : '');
    setEditActive(k.is_active);
  }

  async function onSaveEdit() {
    if (!editKey) return;
    setSaving(true);
    setError(null);
    try {
      const body: Parameters<typeof updateKey>[1] = {};
      if (editName !== editKey.name) body.name = editName;
      body.team_id = editTeamId || null;
      body.user_id = editUserId || null;
      body.models = editModelsAllowAll ? ['*'] : editModelSelections;
      body.rpm_limit = editRpmLimit ? parseInt(editRpmLimit) : null;
      body.tpm_limit = editTpmLimit ? parseInt(editTpmLimit) : null;
      body.max_budget = editMaxBudget ? parseFloat(editMaxBudget) : null;
      body.is_active = editActive;
      await updateKey(editKey.id, body);
      setEditKey(null);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Update failed');
    } finally {
      setSaving(false);
    }
  }

  function handleCreateClick() {
    if (typeof window !== 'undefined') {
      localStorage.setItem('gatewayllm_tip_keys-create', 'true');
    }
    setShowForm(!showForm);
  }

  return (
    <div className="space-y-6">
      <FirstVisitTip tipId="keys-create" title="Click Create Key to generate your first token">
        Click <strong>Create Key</strong>, copy the token that appears (shown exactly once), and paste
        it into your app as <code>OPENAI_API_KEY</code>. We&apos;ll also show curl and Python snippets
        right after you create it.
      </FirstVisitTip>
      <PageHeader
        title="API Keys"
        description={isViewer ? 'View API keys. Read-only access.' : 'Create and manage keys for client access.'}
        action={
          canWrite ? (
            <button type="button" className="gatewayllm-btn" onClick={handleCreateClick}>
              {showForm ? 'Cancel' : 'Create Key'}
            </button>
          ) : undefined
        }
      />

      {isViewer && (
        <div className="rounded-lg border border-sky-800/40 bg-sky-950/20 px-4 py-2.5 text-sm text-sky-300">
          Read-only access — contact an admin for write permissions.
        </div>
      )}

      {error && <Alert variant="error" onDismiss={() => setError(null)}>{error}</Alert>}

      {revealed && (
        <div className="gatewayllm-card border-emerald-900/50 bg-emerald-950/20 p-4">
          <p className="text-sm font-medium text-emerald-200">Key created — copy it now (shown once):</p>
          <div className="mt-2 flex items-center gap-2">
            <code className="flex-1 break-all rounded-lg border border-emerald-900/50 bg-zinc-950 p-3 font-mono text-sm text-emerald-100">
              {revealed}
            </code>
            <CopyButton value={revealed} label="Copy key" />
          </div>
          <TryItPanel rawKey={revealed} modelAlias={deployments[0]?.model_alias} />
          <div className="mt-3 flex gap-2">
            <button
              type="button"
              className="gatewayllm-btn-secondary text-xs"
              onClick={() => setShowKeyModal(true)}
            >
              Open in modal
            </button>
            <button type="button" className="gatewayllm-btn-secondary text-xs" onClick={() => setRevealed(null)}>
              Dismiss
            </button>
          </div>
        </div>
      )}

      {showKeyModal && revealed && (
        <KeyCreatedModal
          rawKey={revealed}
          modelAlias={deployments[0]?.model_alias}
          onClose={() => setShowKeyModal(false)}
        />
      )}

      {showForm && canWrite && (
        <div className="gatewayllm-card p-5 space-y-4">
          <h2 className="text-lg font-medium text-white">New API Key</h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Name</label>
              <input className="gatewayllm-input" value={name} onChange={(e) => setName(e.target.value)} placeholder="production-app" />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Team</label>
              <select value={teamId} onChange={(e) => setTeamId(e.target.value)} className="gatewayllm-input">
                <option value="">No team</option>
                {teams.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
              </select>
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Assigned User</label>
              <select value={userId} onChange={(e) => setUserId(e.target.value)} className="gatewayllm-input">
                <option value="">Unassigned</option>
                {users.filter((u) => u.is_active).map((u) => <option key={u.id} value={u.id}>{u.email}</option>)}
              </select>
            </div>
            <div className="col-span-2">
              <label className="block text-sm text-zinc-400 mb-1">Allowed Models</label>
              <ModelsMultiSelect
                aliases={uniqueAliases}
                allowAll={modelsAllowAll}
                selections={modelSelections}
                onAllowAll={(v) => setModelsAllowAll(v)}
                onToggle={(a) => toggleAlias(a, modelSelections, setModelSelections)}
              />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">RPM Limit</label>
              <input type="number" className="gatewayllm-input" value={rpmLimit} onChange={(e) => setRpmLimit(e.target.value)} placeholder="60" />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">TPM Limit</label>
              <input type="number" className="gatewayllm-input" value={tpmLimit} onChange={(e) => setTpmLimit(e.target.value)} placeholder="100000" />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Max Budget (USD)</label>
              <input type="number" step="0.01" className="gatewayllm-input" value={maxBudget} onChange={(e) => setMaxBudget(e.target.value)} placeholder="100.00" />
            </div>
          </div>
          <button type="button" className="gatewayllm-btn disabled:opacity-50" disabled={creating || !name.trim()} onClick={onCreate}>
            {creating ? 'Creating...' : 'Create Key'}
          </button>
        </div>
      )}

      {/* Edit Modal */}
      {editKey && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm" onClick={() => setEditKey(null)}>
          <div className="w-full max-w-lg rounded-xl border border-zinc-700 bg-zinc-900 p-6 shadow-2xl space-y-4" onClick={(e) => e.stopPropagation()}>
            <h2 className="text-lg font-semibold text-white">Edit API Key</h2>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm text-zinc-400 mb-1">Name</label>
                <input className="gatewayllm-input" value={editName} onChange={(e) => setEditName(e.target.value)} />
              </div>
              <div>
                <label className="block text-sm text-zinc-400 mb-1">Team</label>
                <select value={editTeamId} onChange={(e) => setEditTeamId(e.target.value)} className="gatewayllm-input">
                  <option value="">No team</option>
                  {teams.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
                </select>
              </div>
              <div>
                <label className="block text-sm text-zinc-400 mb-1">Assigned User</label>
                <select value={editUserId} onChange={(e) => setEditUserId(e.target.value)} className="gatewayllm-input">
                  <option value="">Unassigned</option>
                  {users.filter((u) => u.is_active).map((u) => <option key={u.id} value={u.id}>{u.email}</option>)}
                </select>
              </div>
              <div className="col-span-2">
                <label className="block text-sm text-zinc-400 mb-1">Allowed Models</label>
                <ModelsMultiSelect
                  aliases={uniqueAliases}
                  allowAll={editModelsAllowAll}
                  selections={editModelSelections}
                  onAllowAll={(v) => setEditModelsAllowAll(v)}
                  onToggle={(a) => toggleAlias(a, editModelSelections, setEditModelSelections)}
                />
              </div>
              <div>
                <label className="block text-sm text-zinc-400 mb-1">RPM Limit</label>
                <input type="number" className="gatewayllm-input" value={editRpmLimit} onChange={(e) => setEditRpmLimit(e.target.value)} placeholder="No limit" />
              </div>
              <div>
                <label className="block text-sm text-zinc-400 mb-1">TPM Limit</label>
                <input type="number" className="gatewayllm-input" value={editTpmLimit} onChange={(e) => setEditTpmLimit(e.target.value)} placeholder="No limit" />
              </div>
              <div>
                <label className="block text-sm text-zinc-400 mb-1">Max Budget (USD)</label>
                <input type="number" step="0.01" className="gatewayllm-input" value={editMaxBudget} onChange={(e) => setEditMaxBudget(e.target.value)} placeholder="No limit" />
              </div>
              <div className="flex items-end pb-1">
                <label className="flex items-center gap-2 cursor-pointer">
                  <input type="checkbox" checked={editActive} onChange={(e) => setEditActive(e.target.checked)} className="rounded border-zinc-600" />
                  <span className="text-sm text-zinc-300">Active</span>
                </label>
              </div>
            </div>
            <div className="flex justify-end gap-3 pt-2">
              <button type="button" className="gatewayllm-btn-secondary" onClick={() => setEditKey(null)}>Cancel</button>
              <button type="button" className="gatewayllm-btn disabled:opacity-50" disabled={saving || !editName.trim()} onClick={onSaveEdit}>
                {saving ? 'Saving...' : 'Save Changes'}
              </button>
            </div>
          </div>
        </div>
      )}

      <div className="gatewayllm-table-wrap">
        <table className="gatewayllm-table">
          <thead className="bg-zinc-900/80">
            <tr>
              <th className="gatewayllm-th">Name</th>
              <th className="gatewayllm-th">Team</th>
              <th className="gatewayllm-th">User</th>
              <th className="gatewayllm-th">Models</th>
              <th className="gatewayllm-th text-right">Spend</th>
              <th className="gatewayllm-th text-right">Budget</th>
              <th className="gatewayllm-th">Status</th>
              <th className="gatewayllm-th">Created</th>
              {canWrite && <th className="gatewayllm-th w-36" />}
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-800 bg-zinc-950/40">
            {loading ? (
              <tr>
                <td colSpan={canWrite ? 9 : 8} className="gatewayllm-td text-zinc-500">Loading...</td>
              </tr>
            ) : keys.length === 0 ? (
              <tr>
                <td colSpan={canWrite ? 9 : 8}>
                  <EmptyState
                    message="No API keys yet."
                    action={canWrite ? () => setShowForm(true) : undefined}
                    actionLabel={canWrite ? 'Create your first key' : undefined}
                  />
                </td>
              </tr>
            ) : (
              keys.map((k) => (
                <tr key={k.id} className="hover:bg-zinc-900/40">
                  <td className="gatewayllm-td font-medium">
                    <Link
                      href={`/keys/${k.id}`}
                      className="text-emerald-300 hover:text-emerald-200 hover:underline"
                    >
                      {k.name}
                    </Link>
                  </td>
                  <td className="gatewayllm-td text-zinc-400">
                    {k.team_id ? teamById.get(k.team_id) ?? k.team_id.slice(0, 8) : '—'}
                  </td>
                  <td className="gatewayllm-td text-zinc-400">
                    {k.user_id ? userById.get(k.user_id) ?? k.user_id.slice(0, 8) : '—'}
                  </td>
                  <td className="gatewayllm-td text-xs text-zinc-400">
                    {k.models && k.models.length > 0 ? k.models.join(', ') : 'all'}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums">
                    <Link
                      href={`/keys/${k.id}`}
                      className="text-zinc-300 hover:text-emerald-300"
                      title="View usage and spend logs"
                    >
                      ${k.total_spend.toFixed(4)}
                    </Link>
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums text-zinc-400">
                    {k.max_budget != null ? `$${k.max_budget.toFixed(2)}` : '—'}
                  </td>
                  <td className="gatewayllm-td">
                    <Badge variant={k.is_active ? 'success' : 'default'}>
                      {k.is_active ? 'active' : 'inactive'}
                    </Badge>
                  </td>
                  <td className="gatewayllm-td text-zinc-400">{new Date(k.created_at).toLocaleString()}</td>
                  {canWrite && (
                    <td className="gatewayllm-td text-right space-x-2">
                      <button type="button" className="text-xs text-emerald-400 hover:text-emerald-300" onClick={() => openEdit(k)}>
                        Edit
                      </button>
                      <button
                        type="button"
                        className={`text-xs ${k.is_active ? 'text-amber-400 hover:text-amber-300' : 'text-emerald-400 hover:text-emerald-300'}`}
                        onClick={() => onToggle(k)}
                      >
                        {k.is_active ? 'Disable' : 'Enable'}
                      </button>
                      <button type="button" className="gatewayllm-btn-danger" onClick={() => onDelete(k.id)}>
                        Delete
                      </button>
                    </td>
                  )}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ModelsMultiSelect({
  aliases,
  allowAll,
  selections,
  onAllowAll,
  onToggle,
}: {
  aliases: string[];
  allowAll: boolean;
  selections: string[];
  onAllowAll: (v: boolean) => void;
  onToggle: (alias: string) => void;
}) {
  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-950/40 p-3">
      <label className="flex items-center gap-2 rounded-md border border-emerald-500/30 bg-emerald-500/5 px-2 py-1.5 cursor-pointer">
        <input
          type="checkbox"
          checked={allowAll}
          onChange={(e) => onAllowAll(e.target.checked)}
          className="rounded border-zinc-600"
        />
        <span className="text-sm text-emerald-200">All models (<code>*</code>)</span>
      </label>
      {!allowAll && (
        <div className="mt-2 max-h-40 overflow-y-auto grid grid-cols-2 gap-1">
          {aliases.length === 0 ? (
            <p className="col-span-2 text-xs text-zinc-500 px-1 py-2">
              No model routes yet. Create routes to restrict this key to specific aliases.
            </p>
          ) : (
            aliases.map((a) => (
              <label
                key={a}
                className="flex items-center gap-2 rounded px-2 py-1 cursor-pointer hover:bg-zinc-800/60"
              >
                <input
                  type="checkbox"
                  checked={selections.includes(a)}
                  onChange={() => onToggle(a)}
                  className="rounded border-zinc-600"
                />
                <span className="font-mono text-xs text-zinc-200">{a}</span>
              </label>
            ))
          )}
        </div>
      )}
      <p className="mt-2 text-[11px] text-zinc-500">
        {allowAll
          ? 'This key can call any model alias configured in your account.'
          : selections.length > 0
          ? `Restricted to ${selections.length} alias${selections.length === 1 ? '' : 'es'}.`
          : 'Pick at least one alias, or switch back to “All models”.'}
      </p>
    </div>
  );
}

function TryItPanel({ rawKey, modelAlias }: { rawKey: string; modelAlias?: string }) {
  const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
  const alias = modelAlias?.trim() || 'gpt-4o-mini';
  const curl = `curl ${apiUrl}/v1/chat/completions \\
  -H "Authorization: Bearer ${rawKey}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${alias}",
    "messages": [{"role":"user","content":"Hello!"}]
  }'`;
  return (
    <div className="mt-3 rounded-lg border border-zinc-800 bg-zinc-950/60 p-3">
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs font-medium uppercase tracking-wide text-zinc-500">Try it now</p>
        <CopyButton value={curl} label="Copy curl" />
      </div>
      <pre className="mt-2 overflow-x-auto font-mono text-[11px] leading-relaxed text-zinc-300">
        {curl}
      </pre>
      {!modelAlias && (
        <p className="mt-1 text-[11px] text-zinc-500">
          Create a model route first so <code className="text-zinc-300">{alias}</code> resolves.
        </p>
      )}
    </div>
  );
}
