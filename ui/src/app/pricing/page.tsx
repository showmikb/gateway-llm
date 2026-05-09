'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  countersignDiscount,
  declareDiscount,
  deletePricing,
  getDiscounts,
  getOperatorCatalog,
  getPricing,
  revokeDiscount,
  setPricing,
} from '@/lib/api';
import { useAuth } from '@/contexts/AuthContext';
import type {
  ModelPricing,
  OperatorPriceCatalog,
  OrgProviderDiscount,
} from '@/lib/types';
import { Alert, PageHeader } from '@/components/ui';

type Tab = 'catalog' | 'discounts';

function formatTokenCost(n: number) {
  if (n === 0) return '0';
  if (Math.abs(n) >= 0.0001) return n.toFixed(8);
  return n.toExponential(4);
}

function discountStatus(d: OrgProviderDiscount): 'declared' | 'countersigned' | 'expired' {
  const now = Date.now();
  if (d.effective_to) {
    const end = new Date(d.effective_to).getTime();
    if (!Number.isNaN(end) && end < now) return 'expired';
  }
  return d.attested_at ? 'countersigned' : 'declared';
}

export default function PricingPage() {
  const { isMasterKey } = useAuth();
  const [tab, setTab] = useState<Tab>('catalog');
  const [error, setError] = useState<string | null>(null);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Pricing"
        description="Vendor list prices are operator-signed and authoritative for billing math. Per-org negotiated discounts apply on top, after operator countersign."
      />

      {error && (
        <Alert variant="error" onDismiss={() => setError(null)}>
          {error}
        </Alert>
      )}

      <div className="flex gap-1 border-b border-zinc-800">
        <TabButton active={tab === 'catalog'} onClick={() => setTab('catalog')}>
          Vendor list prices
        </TabButton>
        <TabButton active={tab === 'discounts'} onClick={() => setTab('discounts')}>
          My discounts
        </TabButton>
      </div>

      {tab === 'catalog' ? (
        <CatalogTab isMasterKey={isMasterKey} setError={setError} />
      ) : (
        <DiscountsTab isMasterKey={isMasterKey} setError={setError} />
      )}
    </div>
  );
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`-mb-px px-4 py-2 text-sm transition ${
        active
          ? 'border-b-2 border-emerald-400 text-white'
          : 'border-b-2 border-transparent text-zinc-400 hover:text-zinc-200'
      }`}
    >
      {children}
    </button>
  );
}

// ---------- Catalog tab ----------

function CatalogTab({
  isMasterKey,
  setError,
}: {
  isMasterKey: boolean;
  setError: (s: string | null) => void;
}) {
  const [signed, setSigned] = useState<OperatorPriceCatalog[]>([]);
  const [custom, setCustom] = useState<ModelPricing[]>([]);
  const [loading, setLoading] = useState(true);
  const [edit, setEdit] = useState<ModelPricing | null>(null);
  const [inputTok, setInputTok] = useState('');
  const [outputTok, setOutputTok] = useState('');
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setError(null);
    const tasks: [Promise<ModelPricing[]>, Promise<OperatorPriceCatalog[]>] = [
      getPricing(),
      // Catalog is master-key gated; fall back to empty for org admins.
      isMasterKey ? getOperatorCatalog() : Promise.resolve<OperatorPriceCatalog[]>([]),
    ];
    const [pricingRows, catalogRows] = await Promise.all(tasks);
    setCustom(pricingRows);
    setSigned(catalogRows);
  }, [isMasterKey, setError]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        setLoading(true);
        await load();
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load pricing');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [load, setError]);

  // Map signed catalog by provider/model so we can mark which built-in
  // rows have an operator-signed override sitting on top.
  const signedByKey = useMemo(() => {
    const m = new Map<string, OperatorPriceCatalog>();
    for (const r of signed) m.set(`${r.provider}/${r.model}`, r);
    return m;
  }, [signed]);

  function openEdit(p: ModelPricing) {
    setEdit(p);
    setInputTok(String(p.input_cost_per_token));
    setOutputTok(String(p.output_cost_per_token));
  }

  async function saveEdit() {
    if (!edit) return;
    const inp = parseFloat(inputTok);
    const out = parseFloat(outputTok);
    if (Number.isNaN(inp) || Number.isNaN(out)) {
      setError('Input and output costs must be valid numbers (USD per token).');
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await setPricing(edit.provider, edit.model, {
        input_cost_per_token: inp,
        output_cost_per_token: out,
      });
      setEdit(null);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  }

  async function resetCustom(p: ModelPricing) {
    if (!confirm(`Remove custom pricing override for ${p.provider}/${p.model}?`)) return;
    setError(null);
    try {
      await deletePricing(p.provider, p.model);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Reset failed');
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 rounded-md border border-zinc-800 bg-zinc-900/40 p-3 text-xs text-zinc-400">
        <span className="rounded-full bg-emerald-500/10 px-2 py-0.5 font-medium text-emerald-300">
          signed
        </span>
        <span>
          rows are operator-signed and used for billing math. Org admins see them as read-only.
        </span>
        {!isMasterKey && (
          <span className="ml-auto rounded-md bg-zinc-800 px-2 py-1 font-mono text-[11px] text-zinc-300">
            read-only · master-key required to edit
          </span>
        )}
      </div>

      <div className="gatewayllm-table-wrap">
        <table className="gatewayllm-table">
          <thead className="bg-zinc-900/80">
            <tr>
              <th className="gatewayllm-th">Provider</th>
              <th className="gatewayllm-th">Model</th>
              <th className="gatewayllm-th">Mode</th>
              <th className="gatewayllm-th text-right">Input $ / token</th>
              <th className="gatewayllm-th text-right">Output $ / token</th>
              <th className="gatewayllm-th">Source</th>
              {isMasterKey && <th className="gatewayllm-th w-40" />}
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-800 bg-zinc-950/40">
            {loading ? (
              <tr>
                <td colSpan={isMasterKey ? 7 : 6} className="gatewayllm-td text-zinc-500">
                  Loading…
                </td>
              </tr>
            ) : custom.length === 0 ? (
              <tr>
                <td colSpan={isMasterKey ? 7 : 6} className="gatewayllm-td text-zinc-500">
                  No pricing entries.
                </td>
              </tr>
            ) : (
              custom.map((p) => {
                const s = signedByKey.get(`${p.provider}/${p.model}`);
                return (
                  <tr key={`${p.provider}/${p.model}`} className="hover:bg-zinc-900/40">
                    <td className="gatewayllm-td capitalize text-zinc-200">{p.provider}</td>
                    <td className="gatewayllm-td font-mono text-sm text-zinc-300">{p.model}</td>
                    <td className="gatewayllm-td text-zinc-500">{p.mode}</td>
                    <td className="gatewayllm-td text-right font-mono text-xs text-zinc-300">
                      {formatTokenCost(p.input_cost_per_token)}
                    </td>
                    <td className="gatewayllm-td text-right font-mono text-xs text-zinc-300">
                      {formatTokenCost(p.output_cost_per_token)}
                    </td>
                    <td className="gatewayllm-td text-xs">
                      {s ? (
                        <span
                          className="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 font-medium text-emerald-300"
                          title={
                            s.signed_at
                              ? `Signed ${new Date(s.signed_at).toLocaleString()} by ${s.signed_by_key_id ?? 'operator'}`
                              : 'Operator-signed'
                          }
                        >
                          signed
                        </span>
                      ) : (
                        <span className="text-zinc-500">built-in</span>
                      )}
                    </td>
                    {isMasterKey && (
                      <td className="gatewayllm-td text-right">
                        <div className="flex justify-end gap-2">
                          <button
                            type="button"
                            className="gatewayllm-btn-secondary px-3 py-1 text-xs"
                            onClick={() => openEdit(p)}
                          >
                            Edit
                          </button>
                          <button
                            type="button"
                            className="rounded-lg px-3 py-1 text-xs text-zinc-500 ring-1 ring-zinc-700 hover:bg-zinc-900 hover:text-zinc-300"
                            onClick={() => resetCustom(p)}
                          >
                            Reset
                          </button>
                        </div>
                      </td>
                    )}
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>

      {edit ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
          <div className="w-full max-w-md gatewayllm-card p-6 shadow-2xl">
            <h3 className="text-lg font-medium text-white">Custom pricing</h3>
            <p className="mt-1 font-mono text-sm text-emerald-300">
              {edit.provider} / {edit.model}
            </p>
            <p className="mt-2 text-xs text-zinc-500">
              Values are USD per token. Operator-signed rates take precedence over manual edits
              once the catalog is loaded.
            </p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="mb-1 block text-xs font-medium text-zinc-500">
                  Input cost / token
                </label>
                <input
                  className="gatewayllm-input font-mono"
                  value={inputTok}
                  onChange={(e) => setInputTok(e.target.value)}
                />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium text-zinc-500">
                  Output cost / token
                </label>
                <input
                  className="gatewayllm-input font-mono"
                  value={outputTok}
                  onChange={(e) => setOutputTok(e.target.value)}
                />
              </div>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button
                type="button"
                className="gatewayllm-btn-secondary"
                onClick={() => setEdit(null)}
                disabled={saving}
              >
                Cancel
              </button>
              <button
                type="button"
                className="gatewayllm-btn"
                onClick={saveEdit}
                disabled={saving}
              >
                {saving ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

// ---------- Discounts tab ----------

function DiscountsTab({
  isMasterKey,
  setError,
}: {
  isMasterKey: boolean;
  setError: (s: string | null) => void;
}) {
  const [rows, setRows] = useState<OrgProviderDiscount[]>([]);
  const [loading, setLoading] = useState(true);
  const [showDeclare, setShowDeclare] = useState(false);
  const [provider, setProvider] = useState('');
  const [pct, setPct] = useState('');
  const [evidence, setEvidence] = useState('');
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setError(null);
    const data = await getDiscounts();
    setRows(data);
  }, [setError]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        setLoading(true);
        await load();
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load discounts');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [load, setError]);

  async function submitDeclare() {
    const p = provider.trim();
    const v = parseFloat(pct);
    if (!p) {
      setError('Provider is required.');
      return;
    }
    if (Number.isNaN(v) || v < 0 || v > 1) {
      setError('Discount must be a fraction between 0 and 1 (e.g. 0.15 = 15%).');
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await declareDiscount({
        provider: p,
        discount_pct: v,
        evidence_url: evidence.trim() || undefined,
      });
      setShowDeclare(false);
      setProvider('');
      setPct('');
      setEvidence('');
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Declare failed');
    } finally {
      setSaving(false);
    }
  }

  async function countersign(id: string) {
    setError(null);
    try {
      await countersignDiscount(id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Countersign failed');
    }
  }

  async function revoke(id: string) {
    if (!confirm('Revoke this discount? It will no longer apply to billing math.')) return;
    setError(null);
    try {
      await revokeDiscount(id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Revoke failed');
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 rounded-md border border-zinc-800 bg-zinc-900/40 p-3 text-xs text-zinc-400">
        <span>
          Declare per-provider negotiated rates. They only apply to baseline-vs-actual cost math
          after the operator countersigns.
        </span>
        <button
          type="button"
          className="ml-auto gatewayllm-btn-secondary px-3 py-1 text-xs"
          onClick={() => setShowDeclare(true)}
        >
          + Declare discount
        </button>
      </div>

      <div className="gatewayllm-table-wrap">
        <table className="gatewayllm-table">
          <thead className="bg-zinc-900/80">
            <tr>
              <th className="gatewayllm-th">Provider</th>
              <th className="gatewayllm-th text-right">Discount</th>
              <th className="gatewayllm-th">Status</th>
              <th className="gatewayllm-th">Evidence</th>
              <th className="gatewayllm-th">Declared</th>
              <th className="gatewayllm-th">Attested</th>
              <th className="gatewayllm-th w-48" />
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-800 bg-zinc-950/40">
            {loading ? (
              <tr>
                <td colSpan={7} className="gatewayllm-td text-zinc-500">
                  Loading…
                </td>
              </tr>
            ) : rows.length === 0 ? (
              <tr>
                <td colSpan={7} className="gatewayllm-td text-zinc-500">
                  No discounts declared yet.
                </td>
              </tr>
            ) : (
              rows.map((d) => {
                const status = discountStatus(d);
                return (
                  <tr key={d.id} className="hover:bg-zinc-900/40">
                    <td className="gatewayllm-td capitalize text-zinc-200">{d.provider}</td>
                    <td className="gatewayllm-td text-right font-mono text-zinc-300">
                      {(d.discount_pct * 100).toFixed(2)}%
                    </td>
                    <td className="gatewayllm-td">
                      <span
                        className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
                          status === 'countersigned'
                            ? 'bg-emerald-500/10 text-emerald-300'
                            : status === 'expired'
                              ? 'bg-zinc-800 text-zinc-400'
                              : 'bg-amber-500/10 text-amber-300'
                        }`}
                      >
                        {status}
                      </span>
                    </td>
                    <td className="gatewayllm-td text-xs text-zinc-500">
                      {d.evidence_url ? (
                        <a
                          href={d.evidence_url}
                          target="_blank"
                          rel="noreferrer"
                          className="text-zinc-300 underline-offset-2 hover:underline"
                        >
                          link
                        </a>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td className="gatewayllm-td text-xs text-zinc-500">
                      {d.declared_at ? new Date(d.declared_at).toLocaleString() : '—'}
                    </td>
                    <td className="gatewayllm-td text-xs text-zinc-500">
                      {d.attested_at ? new Date(d.attested_at).toLocaleString() : '—'}
                    </td>
                    <td className="gatewayllm-td text-right">
                      <div className="flex justify-end gap-2">
                        {isMasterKey && status !== 'countersigned' && status !== 'expired' && (
                          <button
                            type="button"
                            className="gatewayllm-btn-secondary px-3 py-1 text-xs"
                            onClick={() => countersign(d.id)}
                          >
                            Countersign
                          </button>
                        )}
                        <button
                          type="button"
                          className="rounded-lg px-3 py-1 text-xs text-zinc-500 ring-1 ring-zinc-700 hover:bg-zinc-900 hover:text-zinc-300"
                          onClick={() => revoke(d.id)}
                        >
                          Revoke
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>

      {showDeclare ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
          <div className="w-full max-w-md gatewayllm-card p-6 shadow-2xl">
            <h3 className="text-lg font-medium text-white">Declare a negotiated discount</h3>
            <p className="mt-1 text-xs text-zinc-500">
              The operator must countersign before this affects billing math.
            </p>
            <div className="mt-4 space-y-3">
              <div>
                <label className="mb-1 block text-xs font-medium text-zinc-500">
                  Provider
                </label>
                <input
                  className="gatewayllm-input font-mono"
                  placeholder="openai, anthropic, vertex, …"
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium text-zinc-500">
                  Discount fraction (0–1)
                </label>
                <input
                  className="gatewayllm-input font-mono"
                  placeholder="0.15 for 15% off"
                  value={pct}
                  onChange={(e) => setPct(e.target.value)}
                />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium text-zinc-500">
                  Evidence URL <span className="text-zinc-600">(optional)</span>
                </label>
                <input
                  className="gatewayllm-input"
                  placeholder="https://…/master-services-agreement.pdf"
                  value={evidence}
                  onChange={(e) => setEvidence(e.target.value)}
                />
              </div>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button
                type="button"
                className="gatewayllm-btn-secondary"
                onClick={() => setShowDeclare(false)}
                disabled={saving}
              >
                Cancel
              </button>
              <button
                type="button"
                className="gatewayllm-btn"
                onClick={submitDeclare}
                disabled={saving}
              >
                {saving ? 'Submitting…' : 'Declare'}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
