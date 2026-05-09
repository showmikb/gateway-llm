'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  getDeployments,
  createDeployment,
  updateDeployment,
  deleteDeployment,
  getCredentials,
  getOrganizations,
  discoverProviderModels,
  syncRoutes,
  getPricing,
  updateAliasStrategy,
  listProviders,
} from '@/lib/api';
import type {
  DeploymentRow,
  ProviderCredential,
  Organization,
  DiscoveredModel,
  ModelPricing,
  ProviderInfo,
} from '@/lib/types';
import { PageHeader, Alert, LoadingSkeleton, Badge, InfoTooltip } from '@/components/ui';
import { FirstVisitTip } from '@/components/FirstVisitTip';
import { ModelPicker } from '@/components/ModelPicker';
import { MultiModelPicker } from '@/components/MultiModelPicker';
import { useAuth } from '@/contexts/AuthContext';
import { notifySetupChanged } from '@/lib/setupState';

export default function ModelsPage() {
  const { isAdmin, isViewer } = useAuth();
  const canWrite = isAdmin && !isViewer;

  const [rows, setRows] = useState<DeploymentRow[]>([]);
  const [creds, setCreds] = useState<ProviderCredential[]>([]);
  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [showSync, setShowSync] = useState(false);
  const [createMode, setCreateMode] = useState<'new' | 'add-target'>('new');
  const [creating, setCreating] = useState(false);
  const [wizardStep, setWizardStep] = useState<1 | 2 | 3>(1);

  const [alias, setAlias] = useState('');
  const [orgId, setOrgId] = useState('');
  const [routingStrategy, setRoutingStrategy] = useState('round-robin');
  const [selectedCredentialIds, setSelectedCredentialIds] = useState<Set<string>>(new Set());
  const [selectedModelsByCred, setSelectedModelsByCred] = useState<Map<string, Set<string>>>(new Map());
  const [pricing, setPricingList] = useState<ModelPricing[]>([]);

  const providerById = useMemo(() => {
    const m = new Map<string, ProviderInfo>();
    for (const p of providers) m.set(p.id, p);
    return m;
  }, [providers]);

  // Sync modal state
  const [syncCredId, setSyncCredId] = useState('');
  const [syncOrgId, setSyncOrgId] = useState('');
  const [discovered, setDiscovered] = useState<DiscoveredModel[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [discovering, setDiscovering] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [syncResult, setSyncResult] = useState<{ created: number; skipped: number } | null>(null);

  // Edit modal state
  const [editRow, setEditRow] = useState<DeploymentRow | null>(null);
  const [editAlias, setEditAlias] = useState('');
  const [editProvider, setEditProvider] = useState('openai');
  const [editProviderModel, setEditProviderModel] = useState('');
  const [editCredentialId, setEditCredentialId] = useState('');
  const [editApiKeyEnv, setEditApiKeyEnv] = useState('');
  const [editApiBase, setEditApiBase] = useState('');
  const [editPriority, setEditPriority] = useState('0');
  const [editActive, setEditActive] = useState(true);
  const [saving, setSaving] = useState(false);

  // Model suggestions: catalog models + live discovery per credential
  const [discoveredByCredId, setDiscoveredByCredId] = useState<Map<string, DiscoveredModel[]>>(new Map());
  const discoveryInflight = useRef<Set<string>>(new Set());

  // Per-credential discovery cache. The ModelPicker performs its own
  // discovery against the chosen credential, but we still pre-warm here
  // when the user picks a credential in the wizard so the picker never
  // shows a "loading" flash on first render.
  const discoverForCredential = useCallback(async (credId: string) => {
    if (!credId || discoveredByCredId.has(credId) || discoveryInflight.current.has(credId)) return;
    discoveryInflight.current.add(credId);
    try {
      const models = await discoverProviderModels(credId);
      setDiscoveredByCredId((prev) => new Map(prev).set(credId, models));
    } catch { /* non-critical — picker will retry */ }
    finally { discoveryInflight.current.delete(credId); }
  }, [discoveredByCredId]);

  const orgMap = Object.fromEntries(orgs.map((o) => [o.id, o.name]));

  const existingAliases = useMemo(() => {
    const s = new Set<string>();
    for (const r of rows) s.add(`${r.model_alias}:${r.provider}:${r.org_id ?? ''}`);
    return s;
  }, [rows]);

  const byAlias = useMemo(() => {
    const m = new Map<string, DeploymentRow[]>();
    for (const r of rows) {
      const list = m.get(r.model_alias) ?? [];
      list.push(r);
      m.set(r.model_alias, list);
    }
    Array.from(m.values()).forEach((list) => {
      list.sort((a, b) => a.priority - b.priority);
    });
    return m;
  }, [rows]);

  async function load() {
    setError(null);
    try {
      const [d, o, prov] = await Promise.all([
        getDeployments(),
        getOrganizations(),
        listProviders().catch(() => [] as ProviderInfo[]),
      ]);
      setRows(d);
      setOrgs(o);
      setProviders(prov);
      if (isAdmin) {
        try {
          const [c, p] = await Promise.all([
            getCredentials(),
            getPricing().catch(() => [] as ModelPricing[]),
          ]);
          setCreds(c);
          setPricingList(p);
        } catch {
          // non-admin users don't have access to credentials
        }
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }

  function toggleCredential(credId: string) {
    setSelectedCredentialIds((prev) => {
      const next = new Set(prev);
      if (next.has(credId)) {
        next.delete(credId);
        // also drop any model selections under this credential
        setSelectedModelsByCred((m) => {
          if (!m.has(credId)) return m;
          const copy = new Map(m);
          copy.delete(credId);
          return copy;
        });
      } else {
        next.add(credId);
        discoverForCredential(credId);
      }
      return next;
    });
  }

  function setModelsForCredential(credId: string, models: Set<string>) {
    setSelectedModelsByCred((prev) => {
      const next = new Map(prev);
      if (models.size === 0) next.delete(credId);
      else next.set(credId, models);
      return next;
    });
  }

  // Total number of (credential, model) targets currently selected.
  const totalSelectedModels = useMemo(() => {
    let n = 0;
    selectedModelsByCred.forEach((s) => { n += s.size; });
    return n;
  }, [selectedModelsByCred]);

  function resetComposer() {
    setAlias('');
    setOrgId('');
    setRoutingStrategy('round-robin');
    setSelectedCredentialIds(new Set());
    setSelectedModelsByCred(new Map());
    setCreateMode('new');
    setShowCreate(false);
    setWizardStep(1);
  }

  useEffect(() => {
    load();
  }, []);

  // Flatten Step-1/Step-2 selections into the (credential, provider, model)
  // tuples we will send to the API. Pricing defaults are applied later per
  // target — here we only need the routing identity.
  type PlannedTarget = {
    credentialId: string;
    provider: string;
    providerModel: string;
  };
  const plannedTargets = useMemo<PlannedTarget[]>(() => {
    const out: PlannedTarget[] = [];
    selectedModelsByCred.forEach((modelIds, credId) => {
      const cred = creds.find((c) => c.id === credId);
      if (!cred) return;
      modelIds.forEach((modelId) => {
        const trimmed = modelId.trim();
        if (!trimmed) return;
        out.push({ credentialId: credId, provider: cred.provider, providerModel: trimmed });
      });
    });
    return out;
  }, [selectedModelsByCred, creds]);

  async function handleCreate() {
    if (createMode !== 'add-target' && !alias.trim()) { setError('Virtual model name is required'); return; }
    if (plannedTargets.length === 0) { setError('Select at least one backend model'); return; }

    setCreating(true);
    setError(null);
    const createdIds: string[] = [];

    try {
      for (const t of plannedTargets) {
        const dep = await createDeployment({
          model_alias: alias.trim(),
          provider: t.provider,
          provider_model: t.providerModel,
          credential_id: t.credentialId || undefined,
          org_id: orgId || undefined,
          priority: 0,
          weight: 1,
          routing_strategy: routingStrategy,
        });
        if (dep.id) createdIds.push(dep.id);
      }

      resetComposer();
      notifySetupChanged();
      load();
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Create failed';
      for (const id of createdIds) {
        try { await deleteDeployment(id); } catch { /* best-effort rollback */ }
      }
      setError(
        createdIds.length > 0
          ? `Failed after creating ${createdIds.length} target(s) — rolled back. ${msg}`
          : msg,
      );
    } finally {
      setCreating(false);
    }
  }

  async function handleDelete(id: string) {
    if (!confirm('Delete this target?')) return;
    try {
      await deleteDeployment(id);
      notifySetupChanged();
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Delete failed');
    }
  }

  async function handleAliasStrategy(aliasName: string, strategy: string, existingOrgID: string | null) {
    try {
      await updateAliasStrategy({
        model_alias: aliasName,
        routing_strategy: strategy,
        org_id: existingOrgID || undefined,
      });
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Strategy update failed');
    }
  }

  async function handleInlineSave(d: DeploymentRow, next: Partial<DeploymentRow>) {
    if (!d.id) return;
    try {
      await updateDeployment(d.id, next);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Update failed');
    }
  }

  function openAddTargetFor(aliasName: string) {
    const existing = byAlias.get(aliasName)?.[0];
    setCreateMode('add-target');
    setAlias(aliasName);
    setRoutingStrategy(existing?.routing_strategy || 'round-robin');
    setOrgId(existing?.org_id ?? '');
    setSelectedCredentialIds(new Set());
    setSelectedModelsByCred(new Map());
    setShowCreate(true);
    setShowSync(false);
    setWizardStep(1);
  }

  async function handleToggle(d: DeploymentRow) {
    if (!d.id) return;
    try {
      await updateDeployment(d.id, { is_active: !d.is_active });
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Update failed');
    }
  }

  function openEdit(d: DeploymentRow) {
    setEditRow(d);
    setEditAlias(d.model_alias);
    setEditProvider(d.provider);
    setEditProviderModel(d.provider_model);
    setEditCredentialId(d.credential_id ?? '');
    setEditApiKeyEnv(d.api_key_env ?? '');
    setEditApiBase(d.api_base ?? '');
    setEditPriority(String(d.priority));
    setEditActive(d.is_active !== false);
  }

  async function onSaveEdit() {
    if (!editRow?.id) return;
    setSaving(true);
    setError(null);
    try {
      await updateDeployment(editRow.id, {
        model_alias: editAlias,
        provider: editProvider,
        provider_model: editProviderModel,
        credential_id: editCredentialId || undefined,
        api_key_env: editApiKeyEnv || undefined,
        api_base: editApiBase || undefined,
        priority: parseInt(editPriority) || 0,
        is_active: editActive,
      });
      setEditRow(null);
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Update failed');
    } finally {
      setSaving(false);
    }
  }

  async function handleDiscover() {
    if (!syncCredId) return;
    setDiscovering(true);
    setSyncResult(null);
    try {
      const models = await discoverProviderModels(syncCredId);
      setDiscovered(models);
      const preSelected = new Set<string>();
      for (const m of models) {
        const key = `${m.id}:${m.provider}:${syncOrgId}`;
        if (!existingAliases.has(key)) {
          preSelected.add(m.id);
        }
      }
      setSelected(preSelected);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Discovery failed');
    } finally {
      setDiscovering(false);
    }
  }

  async function handleSync() {
    if (selected.size === 0) return;
    setSyncing(true);
    try {
      const result = await syncRoutes({
        credential_id: syncCredId,
        org_id: syncOrgId || undefined,
        models: Array.from(selected),
        skip_existing: true,
      });
      setSyncResult({ created: result.created, skipped: result.skipped });
      setDiscovered([]);
      setSelected(new Set());
      notifySetupChanged();
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Sync failed');
    } finally {
      setSyncing(false);
    }
  }

  function toggleSelected(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }

  const aliases = Array.from(byAlias.keys()).sort();

  return (
    <div className="space-y-6">
      <FirstVisitTip tipId="models" title="Create a virtual model">
        A virtual model is the model name your apps call (e.g. <code>fast-chat</code>). Add one or
        more backend targets — Gateway-LLM routes each request to the best one using the strategy
        you choose.
      </FirstVisitTip>
      <PageHeader
        title="Virtual Models"
        description={isViewer ? 'View virtual models. Read-only access.' : 'Create virtual models and map them to one or more backend targets.'}
        action={
          canWrite ? (
            <div className="flex gap-2">
              <button onClick={() => { setShowSync(!showSync); resetComposer(); }} className="gatewayllm-btn-secondary">
                {showSync ? 'Cancel Sync' : 'Sync Provider Models'}
              </button>
              <button onClick={() => {
                if (showCreate) {
                  resetComposer();
                } else {
                  setCreateMode('new');
                  setSelectedCredentialIds(new Set());
                  setSelectedModelsByCred(new Map());
                  setShowCreate(true);
                  setShowSync(false);
                  setWizardStep(1);
                }
              }} className="gatewayllm-btn">
                {showCreate ? 'Cancel' : 'Create Virtual Model'}
              </button>
            </div>
          ) : undefined
        }
      />

      {isViewer && (
        <div className="rounded-lg border border-sky-800/40 bg-sky-950/20 px-4 py-2.5 text-sm text-sky-300">
          Read-only access — contact an admin for write permissions.
        </div>
      )}

      {error && <Alert variant="error" onDismiss={() => setError(null)}>{error}</Alert>}

      {showSync && canWrite && (
        <div className="gatewayllm-card p-5 space-y-4">
          <h2 className="text-lg font-medium text-white">Sync Provider Models</h2>
          <p className="text-sm text-zinc-400">Fetch available models from a provider and bulk-create virtual models.</p>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Provider Key<InfoTooltip text="Select the stored API key to use for discovering models from the provider." /></label>
              <select value={syncCredId} onChange={(e) => setSyncCredId(e.target.value)} className="gatewayllm-input">
                <option value="">Select a credential</option>
                {creds.filter((c) => c.is_active).map((c) => (
                  <option key={c.id} value={c.id}>{c.name} ({c.provider})</option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Organization<InfoTooltip text="Scope imported models to a specific org. Leave empty for global." /></label>
              <select value={syncOrgId} onChange={(e) => setSyncOrgId(e.target.value)} className="gatewayllm-input">
                <option value="">Global (all orgs)</option>
                {orgs.filter((o) => o.is_active).map((o) => (
                  <option key={o.id} value={o.id}>{o.name}</option>
                ))}
              </select>
            </div>
          </div>
          <button onClick={handleDiscover} disabled={!syncCredId || discovering} className="gatewayllm-btn disabled:opacity-50">
            {discovering ? 'Fetching...' : 'Fetch Models'}
          </button>

          {syncResult && (
            <Alert variant="success">
              Imported {syncResult.created} model(s), skipped {syncResult.skipped} existing.
            </Alert>
          )}

          {discovered.length > 0 && (
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-sm text-zinc-300">{discovered.length} models found</span>
                <div className="flex gap-2">
                  <button onClick={() => setSelected(new Set(discovered.map((m) => m.id)))} className="text-xs text-emerald-400 hover:text-emerald-300">Select all</button>
                  <button onClick={() => setSelected(new Set())} className="text-xs text-zinc-400 hover:text-zinc-300">Deselect all</button>
                </div>
              </div>
              <div className="max-h-64 overflow-y-auto rounded-lg border border-zinc-800 divide-y divide-zinc-800">
                {discovered.map((m) => {
                  const exists = existingAliases.has(`${m.id}:${m.provider}:${syncOrgId}`);
                  return (
                    <label key={m.id} className={`flex items-center gap-3 px-3 py-2 cursor-pointer hover:bg-zinc-800/40 ${exists ? 'opacity-50' : ''}`}>
                      <input
                        type="checkbox"
                        checked={selected.has(m.id)}
                        onChange={() => toggleSelected(m.id)}
                        disabled={exists}
                        className="rounded border-zinc-600"
                      />
                      <span className="font-mono text-sm text-zinc-200">{m.id}</span>
                      {m.capabilities.length > 0 && (
                        <span className="text-xs text-zinc-500">{m.capabilities.join(', ')}</span>
                      )}
                      {exists && <Badge variant="default">exists</Badge>}
                    </label>
                  );
                })}
              </div>
              <button onClick={handleSync} disabled={selected.size === 0 || syncing} className="gatewayllm-btn disabled:opacity-50">
                {syncing ? 'Importing...' : `Import ${selected.size} model(s)`}
              </button>
            </div>
          )}
        </div>
      )}

      {showCreate && canWrite && (
        <div className="gatewayllm-card p-5 space-y-5">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-medium text-white">
              {createMode === 'add-target' ? `Add target to "${alias}"` : 'New Virtual Model'}
            </h2>
            <ol className="flex items-center gap-2 text-xs">
              {[
                { n: 1, label: 'Credential' },
                { n: 2, label: 'Models' },
                { n: 3, label: 'Name & strategy' },
              ].map((s, i, arr) => (
                <li key={s.n} className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setWizardStep(s.n as 1 | 2 | 3)}
                    className={`flex items-center gap-2 rounded-full px-2.5 py-1 transition-colors ${
                      wizardStep === s.n
                        ? 'bg-emerald-500/15 text-emerald-300'
                        : wizardStep > s.n
                          ? 'text-emerald-400 hover:text-emerald-300'
                          : 'text-zinc-500 hover:text-zinc-400'
                    }`}
                  >
                    <span className={`flex h-5 w-5 items-center justify-center rounded-full text-[11px] font-semibold ${
                      wizardStep === s.n
                        ? 'bg-emerald-500 text-zinc-950'
                        : wizardStep > s.n
                          ? 'bg-emerald-500/30 text-emerald-200'
                          : 'bg-zinc-800 text-zinc-500'
                    }`}>{s.n}</span>
                    <span className="hidden sm:inline">{s.label}</span>
                  </button>
                  {i < arr.length - 1 && <span className="h-px w-4 bg-zinc-700" aria-hidden />}
                </li>
              ))}
            </ol>
          </div>

          {/* Step 1: multi-select the credentials we'll route across. */}
          {wizardStep === 1 && (
            <div className="space-y-4">
              <p className="text-sm text-zinc-400">
                Tick every provider key this virtual model should be allowed to route to. Pick keys
                from multiple providers to fan out across OpenAI, Anthropic, Vertex, etc.
              </p>
              {creds.filter((c) => c.is_active).length === 0 ? (
                <div className="rounded-md border border-amber-800/50 bg-amber-950/20 p-4 text-sm text-amber-200">
                  No provider credentials yet.{' '}
                  <a href="/credentials" className="text-emerald-300 underline-offset-2 hover:underline">
                    Add one in Credentials
                  </a>{' '}
                  before creating a route.
                </div>
              ) : (
                (() => {
                  const visible = creds.filter(
                    (c) => c.is_active && (!orgId || !c.org_id || c.org_id === orgId),
                  );
                  // Group credentials by provider so the user reads "I'll route across these providers".
                  const byProvider = new Map<string, ProviderCredential[]>();
                  for (const c of visible) {
                    const list = byProvider.get(c.provider) ?? [];
                    list.push(c);
                    byProvider.set(c.provider, list);
                  }
                  const orderedProviders = Array.from(byProvider.keys()).sort((a, b) => {
                    const la = providerById.get(a)?.label || a;
                    const lb = providerById.get(b)?.label || b;
                    return la.localeCompare(lb);
                  });
                  return (
                    <div className="space-y-4">
                      {orderedProviders.map((prov) => {
                        const info = providerById.get(prov);
                        const provCreds = byProvider.get(prov) ?? [];
                        return (
                          <div key={prov} className="space-y-2">
                            <div className="flex items-center gap-2 text-xs text-zinc-400">
                              <span className="font-medium uppercase tracking-wide text-zinc-300">
                                {info?.label || prov}
                              </span>
                              {info?.supports_discovery ? (
                                <Badge variant="success">Live discovery</Badge>
                              ) : (
                                <Badge variant="default">Manual</Badge>
                              )}
                            </div>
                            <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                              {provCreds.map((c) => {
                                const checked = selectedCredentialIds.has(c.id);
                                return (
                                  <li key={c.id}>
                                    <label
                                      className={`flex w-full cursor-pointer items-center gap-3 rounded-lg border px-3 py-2 text-left transition-colors ${
                                        checked
                                          ? 'border-emerald-500/50 bg-emerald-500/10'
                                          : 'border-zinc-700 bg-zinc-900/40 hover:border-emerald-500/40'
                                      }`}
                                    >
                                      <input
                                        type="checkbox"
                                        checked={checked}
                                        onChange={() => toggleCredential(c.id)}
                                        className="rounded border-zinc-600"
                                      />
                                      <span className="flex-1">
                                        <span className="block text-sm font-medium text-white">{c.name}</span>
                                        <span className="block text-xs text-zinc-500">{c.api_key_masked}</span>
                                      </span>
                                    </label>
                                  </li>
                                );
                              })}
                            </ul>
                          </div>
                        );
                      })}
                    </div>
                  );
                })()
              )}
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs text-zinc-500">
                  {selectedCredentialIds.size === 0
                    ? 'No credentials selected.'
                    : `${selectedCredentialIds.size} credential${selectedCredentialIds.size === 1 ? '' : 's'} selected.`}
                </span>
                <div className="flex gap-2">
                  <button type="button" onClick={resetComposer} className="gatewayllm-btn-secondary">Cancel</button>
                  <button
                    type="button"
                    onClick={() => setWizardStep(2)}
                    disabled={selectedCredentialIds.size === 0}
                    className="gatewayllm-btn disabled:opacity-50"
                  >
                    Next: choose models
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* Step 2: per-credential, multi-select the backend models. */}
          {wizardStep === 2 && (
            <div className="space-y-4">
              <p className="text-sm text-zinc-400">
                Pick every backend model this virtual model is allowed to route to. Each ticked
                model becomes one routing target — Gateway-LLM picks among them per request based on
                the strategy you choose in the next step.
              </p>
              {selectedCredentialIds.size === 0 ? (
                <div className="rounded-md border border-amber-800/50 bg-amber-950/20 p-4 text-sm text-amber-200">
                  No credentials selected. Go back and pick at least one provider key.
                </div>
              ) : (
                <div className="space-y-3">
                  {Array.from(selectedCredentialIds).map((credId) => {
                    const cred = creds.find((c) => c.id === credId);
                    if (!cred) return null;
                    const info = providerById.get(cred.provider);
                    const picked = selectedModelsByCred.get(credId) ?? new Set<string>();
                    const preloaded = discoveredByCredId.get(credId);
                    return (
                      <div key={credId} className="rounded-lg border border-zinc-700/60 bg-zinc-900/40 p-4 space-y-3">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <div>
                            <div className="text-sm font-medium text-white">
                              {cred.name}
                              <span className="ml-2 text-xs font-normal text-zinc-500">
                                {info?.label || cred.provider} &middot; {cred.api_key_masked}
                              </span>
                            </div>
                          </div>
                          <Badge variant={picked.size > 0 ? 'success' : 'default'}>
                            {picked.size} model{picked.size === 1 ? '' : 's'} selected
                          </Badge>
                        </div>
                        <MultiModelPicker
                          provider={cred.provider}
                          providerInfo={info}
                          credentialId={credId}
                          pricingCatalog={pricing}
                          value={picked}
                          onChange={(next) => setModelsForCredential(credId, next)}
                          preloadedDiscovery={preloaded}
                        />
                      </div>
                    );
                  })}
                </div>
              )}
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs text-zinc-500">
                  {totalSelectedModels === 0
                    ? 'No models selected.'
                    : `${totalSelectedModels} routing target${totalSelectedModels === 1 ? '' : 's'} will be created.`}
                </span>
                <div className="flex gap-2">
                  <button type="button" onClick={() => setWizardStep(1)} className="gatewayllm-btn-secondary">Back</button>
                  <button
                    type="button"
                    onClick={() => setWizardStep(3)}
                    disabled={totalSelectedModels === 0}
                    className="gatewayllm-btn disabled:opacity-50"
                  >
                    Next: name & strategy
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* Step 3: alias + routing strategy */}
          {wizardStep === 3 && (
            <div className="space-y-4">
              {createMode === 'add-target' ? (
                <p className="text-sm text-zinc-400">
                  Adding to <span className="font-mono text-emerald-300">{alias}</span> (strategy: {routingStrategy}).
                </p>
              ) : (
                <>
                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                    <div>
                      <label className="block text-sm text-zinc-400 mb-1">
                        Model name your app calls
                        <InfoTooltip text="The model name your apps pass in the 'model' field. Gateway-LLM routes it to the targets below." />
                      </label>
                      <input
                        value={alias}
                        onChange={(e) => setAlias(e.target.value)}
                        className="gatewayllm-input"
                        placeholder="smart"
                      />
                    </div>
                    <div>
                      <label className="block text-sm text-zinc-400 mb-1">
                        Routing strategy
                        <InfoTooltip text="How the gateway picks between targets. All targets under the same virtual model share one strategy." />
                      </label>
                      <select value={routingStrategy} onChange={(e) => setRoutingStrategy(e.target.value)} className="gatewayllm-input">
                        <option value="round-robin">Round-robin</option>
                        <option value="least-latency">Least-latency</option>
                        <option value="priority">Priority</option>
                        <option value="cheapest">Cheapest</option>
                        <option value="weighted">Weighted</option>
                      </select>
                    </div>
                    <div>
                      <label className="block text-sm text-zinc-400 mb-1">
                        Organization
                        <InfoTooltip text="Scope this virtual model to a specific org. Leave empty for global." />
                      </label>
                      <select value={orgId} onChange={(e) => setOrgId(e.target.value)} className="gatewayllm-input">
                        <option value="">Global (all orgs)</option>
                        {orgs.filter((o) => o.is_active).map((o) => (
                          <option key={o.id} value={o.id}>{o.name}</option>
                        ))}
                      </select>
                    </div>
                  </div>
                  {alias.trim() && (
                    <p className="text-xs text-zinc-500 font-mono bg-zinc-800/50 rounded px-3 py-1.5 inline-block">
                      {'{ "model": "'}<span className="text-emerald-400">{alias.trim()}</span>{'" }'}
                    </p>
                  )}
                </>
              )}
              <div className="rounded-md border border-zinc-800/60 bg-zinc-950/30 p-3 text-xs text-zinc-400">
                <p className="mb-2 font-medium text-zinc-300">
                  {plannedTargets.length} routing target{plannedTargets.length === 1 ? '' : 's'} about to be created
                </p>
                {(() => {
                  // Group preview by provider so the user sees "OpenAI: 3, Anthropic: 2".
                  const byProv = new Map<string, PlannedTarget[]>();
                  for (const t of plannedTargets) {
                    const list = byProv.get(t.provider) ?? [];
                    list.push(t);
                    byProv.set(t.provider, list);
                  }
                  const provs = Array.from(byProv.keys()).sort((a, b) => {
                    const la = providerById.get(a)?.label || a;
                    const lb = providerById.get(b)?.label || b;
                    return la.localeCompare(lb);
                  });
                  return (
                    <div className="space-y-2">
                      {provs.map((prov) => {
                        const items = byProv.get(prov) ?? [];
                        return (
                          <div key={prov}>
                            <p className="text-[11px] uppercase tracking-wide text-emerald-300">
                              {providerById.get(prov)?.label || prov}
                              <span className="ml-2 text-zinc-500">
                                {items.length} model{items.length === 1 ? '' : 's'}
                              </span>
                            </p>
                            <ul className="mt-1 space-y-0.5">
                              {items.map((t, i) => (
                                <li key={`${t.credentialId}/${t.providerModel}/${i}`} className="font-mono text-zinc-300">
                                  {t.providerModel}
                                  <span className="text-zinc-500">  (priority 0, weight 1)</span>
                                </li>
                              ))}
                            </ul>
                          </div>
                        );
                      })}
                      <p className="pt-1 text-[11px] text-zinc-500">
                        Tweak priority, weight, and per-target costs from the row inline editors after creation.
                      </p>
                    </div>
                  );
                })()}
              </div>
              <div className="flex justify-between gap-2">
                <button type="button" onClick={() => setWizardStep(2)} className="gatewayllm-btn-secondary">Back</button>
                <button
                  onClick={handleCreate}
                  disabled={
                    creating ||
                    plannedTargets.length === 0 ||
                    (createMode !== 'add-target' && !alias.trim())
                  }
                  className="gatewayllm-btn disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {creating
                    ? 'Creating...'
                    : createMode === 'add-target'
                      ? `Add ${plannedTargets.length} target(s) to ${alias}`
                      : `Create virtual model with ${plannedTargets.length} target(s)`}
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Edit Modal */}
      {editRow && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm" onClick={() => setEditRow(null)}>
          <div className="w-full max-w-2xl rounded-xl border border-zinc-700 bg-zinc-900 p-6 shadow-2xl space-y-4" onClick={(e) => e.stopPropagation()}>
            <h2 className="text-lg font-semibold text-white">Edit Target</h2>
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
              <div className="space-y-3">
                <div>
                  <label className="block text-sm text-zinc-400 mb-1">Virtual Model</label>
                  <input className="gatewayllm-input" value={editAlias} onChange={(e) => setEditAlias(e.target.value)} />
                </div>
                <div>
                  <label className="block text-sm text-zinc-400 mb-1">Provider</label>
                  <select
                    value={editProvider}
                    onChange={(e) => { setEditProvider(e.target.value); setEditCredentialId(''); setEditProviderModel(''); }}
                    className="gatewayllm-input"
                  >
                    {providers.map((p) => (
                      <option key={p.id} value={p.id}>{p.label}</option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="block text-sm text-zinc-400 mb-1">Provider Key</label>
                  <select
                    value={editCredentialId}
                    onChange={(e) => {
                      const credId = e.target.value;
                      setEditCredentialId(credId);
                      if (credId) discoverForCredential(credId);
                    }}
                    className="gatewayllm-input"
                  >
                    <option value="">None (use env var)</option>
                    {creds
                      .filter((c) => c.provider === editProvider && c.is_active)
                      .map((c) => (
                        <option key={c.id} value={c.id}>{c.name} ({c.api_key_masked})</option>
                      ))}
                  </select>
                </div>
                <div>
                  <label className="block text-sm text-zinc-400 mb-1">API Key Env (fallback)</label>
                  <input className="gatewayllm-input" value={editApiKeyEnv} onChange={(e) => setEditApiKeyEnv(e.target.value)} placeholder={`${(editProvider || 'PROVIDER').toUpperCase()}_API_KEY`} />
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="block text-sm text-zinc-400 mb-1">Priority</label>
                    <input type="number" className="gatewayllm-input" value={editPriority} onChange={(e) => setEditPriority(e.target.value)} />
                  </div>
                  <div className="flex items-end pb-1">
                    <label className="flex items-center gap-2 cursor-pointer">
                      <input type="checkbox" checked={editActive} onChange={(e) => setEditActive(e.target.checked)} className="rounded border-zinc-600" />
                      <span className="text-sm text-zinc-300">Active</span>
                    </label>
                  </div>
                </div>
              </div>
              <div>
                <label className="block text-sm text-zinc-400 mb-1">Backend Model</label>
                <ModelPicker
                  provider={editProvider}
                  providerInfo={providerById.get(editProvider)}
                  credentialId={editCredentialId}
                  pricingCatalog={pricing}
                  value={editProviderModel}
                  onChange={setEditProviderModel}
                />
              </div>
            </div>
            <div className="flex justify-end gap-3 pt-2">
              <button type="button" className="gatewayllm-btn-secondary" onClick={() => setEditRow(null)}>Cancel</button>
              <button type="button" className="gatewayllm-btn disabled:opacity-50" disabled={saving || !editAlias.trim() || !editProviderModel.trim()} onClick={onSaveEdit}>
                {saving ? 'Saving...' : 'Save Changes'}
              </button>
            </div>
          </div>
        </div>
      )}

      {loading ? (
        <LoadingSkeleton rows={3} header={false} />
      ) : aliases.length === 0 ? (
        <div className="gatewayllm-card p-6 text-sm text-zinc-400">
          <p className="text-base font-medium text-zinc-200">No virtual models configured yet.</p>
          <p className="mt-2">
            A <span className="font-medium text-zinc-300">virtual model</span> is the model name
            your apps send in the <code className="text-emerald-300">model</code> field. Attach one
            or more backend targets and a routing strategy, and Gateway-LLM picks the best target
            for every request. For example:
            <span className="mx-1 rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-emerald-300">smart</span>
            with strategy <span className="text-emerald-300">Cheapest</span> routing between
            <span className="mx-1 rounded bg-zinc-800 px-1.5 py-0.5 font-mono">gpt-4o-mini</span>,
            <span className="mx-1 rounded bg-zinc-800 px-1.5 py-0.5 font-mono">gemini-flash</span>, and
            <span className="mx-1 rounded bg-zinc-800 px-1.5 py-0.5 font-mono">claude-haiku</span>.
          </p>
          {canWrite && (
            <div className="mt-4">
              <button type="button" className="gatewayllm-btn" onClick={() => setShowCreate(true)}>
                Create your first virtual model
              </button>
            </div>
          )}
        </div>
      ) : (
        <div className="space-y-6">
          {aliases.map((aliasName) => {
            const targets = byAlias.get(aliasName) ?? [];
            const groupStrategy = targets[0]?.routing_strategy || 'round-robin';
            const groupOrgID = targets[0]?.org_id ?? null;
            return (
              <div key={aliasName} className="gatewayllm-card overflow-hidden">
                <div className="flex flex-wrap items-center justify-between gap-3 border-b border-zinc-800 bg-zinc-900/50 px-5 py-3">
                  <div>
                    <div className="flex items-center gap-3">
                      <h2 className="font-mono text-lg font-medium text-emerald-300">{aliasName}</h2>
                      <span className="text-xs text-zinc-500">{targets.length} target{targets.length === 1 ? '' : 's'}</span>
                    </div>
                    <p className="mt-0.5 text-xs text-zinc-500">
                      Apps call <code className="text-zinc-400">model: &quot;{aliasName}&quot;</code> — Gateway-LLM routes to one of these targets.
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    <label className="text-xs text-zinc-400">Strategy</label>
                    <select
                      value={groupStrategy}
                      onChange={(e) => handleAliasStrategy(aliasName, e.target.value, groupOrgID)}
                      disabled={!canWrite}
                      className="gatewayllm-input h-8 py-0 text-xs"
                    >
                      <option value="round-robin">Round-robin</option>
                      <option value="least-latency">Least-latency</option>
                      <option value="priority">Priority</option>
                      <option value="cheapest">Cheapest</option>
                      <option value="weighted">Weighted</option>
                    </select>
                    {canWrite && (
                      <button
                        type="button"
                        onClick={() => openAddTargetFor(aliasName)}
                        className="gatewayllm-btn-secondary text-xs"
                      >
                        + Add target
                      </button>
                    )}
                  </div>
                </div>
                <div className="gatewayllm-table-wrap border-0">
                  <table className="gatewayllm-table">
                    <thead className="bg-zinc-900/40">
                      <tr>
                        <th className="gatewayllm-th">Provider</th>
                        <th className="gatewayllm-th">Backend Model</th>
                        <th className="gatewayllm-th">Provider Key</th>
                        <th className="gatewayllm-th">Organization</th>
                        <th className="gatewayllm-th">Priority</th>
                        <th className="gatewayllm-th">Weight</th>
                        <th className="gatewayllm-th">Status</th>
                        {canWrite && <th className="gatewayllm-th text-right">Actions</th>}
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-zinc-800">
                      {targets.map((d, i) => (
                        <tr key={d.id || `${d.provider}-${d.provider_model}-${i}`} className="hover:bg-zinc-900/30">
                          <td className="gatewayllm-td capitalize text-zinc-200">{d.provider}</td>
                          <td className="gatewayllm-td font-mono text-sm text-zinc-300">{d.provider_model}</td>
                          <td className="gatewayllm-td text-xs text-zinc-400">{d.credential_name || d.api_key_env || '—'}</td>
                          <td className="gatewayllm-td text-sm text-zinc-400">{d.org_id ? orgMap[d.org_id] || d.org_id : 'Global'}</td>
                          <td className="gatewayllm-td tabular-nums text-zinc-400">
                            {canWrite && d.id ? (
                              <input
                                type="number"
                                defaultValue={d.priority ?? 0}
                                className="gatewayllm-input h-7 w-20 py-0 text-xs"
                                onBlur={(e) => {
                                  const next = parseInt(e.target.value) || 0;
                                  if (next !== d.priority) void handleInlineSave(d, { priority: next });
                                }}
                              />
                            ) : (
                              d.priority
                            )}
                          </td>
                          <td className="gatewayllm-td tabular-nums text-zinc-400">
                            {canWrite && d.id ? (
                              <input
                                type="number"
                                min={1}
                                defaultValue={d.weight ?? 1}
                                className="gatewayllm-input h-7 w-20 py-0 text-xs"
                                onBlur={(e) => {
                                  const next = parseInt(e.target.value) || 1;
                                  if (next !== (d.weight ?? 1)) void handleInlineSave(d, { weight: next });
                                }}
                              />
                            ) : (
                              d.weight ?? 1
                            )}
                          </td>
                          <td className="gatewayllm-td">
                            <Badge variant={d.is_active !== false ? 'success' : 'default'}>
                              {d.is_active !== false ? 'active' : 'inactive'}
                            </Badge>
                          </td>
                          {canWrite && (
                            <td className="gatewayllm-td text-right space-x-2">
                              {d.id && (
                                <>
                                  <button onClick={() => openEdit(d)} className="text-xs text-emerald-400 hover:text-emerald-300">
                                    Edit
                                  </button>
                                  <button
                                    onClick={() => handleToggle(d)}
                                    className={`text-xs ${d.is_active !== false ? 'text-amber-400 hover:text-amber-300' : 'text-emerald-400 hover:text-emerald-300'}`}
                                  >
                                    {d.is_active !== false ? 'Disable' : 'Enable'}
                                  </button>
                                  <button onClick={() => handleDelete(d.id!)} className="text-xs text-red-400 hover:text-red-300">
                                    Delete
                                  </button>
                                </>
                              )}
                            </td>
                          )}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
