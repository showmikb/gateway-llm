'use client';

import Link from 'next/link';
import { useEffect, useMemo, useState } from 'react';
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

import { Alert, PageHeader } from '@/components/ui';
import {
  type SavingsAliasBreakdown,
  type SavingsListResponse,
  getDailySavings,
  getSavings,
} from '@/lib/api';
import type { DailySavings, RoutingSavings } from '@/lib/types';

const PAGE_SIZE = 25;

function fmtUSD(n: number, max = 4): string {
  if (!isFinite(n)) return '$0.00';
  return `$${n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: max })}`;
}

function fmtPct(n: number | null | undefined): string {
  if (n === null || n === undefined) return '—';
  return `${(n * 100).toFixed(1)}%`;
}

function fmtNum(n: number): string {
  return n.toLocaleString();
}

interface HeroStats {
  savings: number;
  baseline: number;
  actual: number;
  routedPct: number;
  qualityPassPct: number | null;
}

function deriveHeroStats(
  rows: RoutingSavings[] | null | undefined,
  alias: SavingsAliasBreakdown[] | null | undefined,
): HeroStats {
  const safeRows = rows ?? [];
  const safeAlias = alias ?? [];
  const fromBreakdown = safeAlias.length > 0;
  let savings = 0;
  let baseline = 0;
  let actual = 0;
  let total = 0;
  let routed = 0;
  const qpScores: number[] = [];

  if (fromBreakdown) {
    for (const a of safeAlias) {
      savings += a.savings_usd;
      baseline += a.baseline_usd;
      actual += a.actual_usd;
      total += a.requests;
      routed += a.routed;
      if (a.quality_pass_pct != null) qpScores.push(a.quality_pass_pct);
    }
  } else {
    for (const r of safeRows) {
      savings += r.savings_usd;
      baseline += r.baseline_cost_usd;
      actual += r.actual_cost_usd;
      total++;
      if (r.overridden) routed++;
      if (r.quality_pass != null) qpScores.push(r.quality_pass ? 1 : 0);
    }
  }

  return {
    savings,
    baseline,
    actual,
    routedPct: total > 0 ? routed / total : 0,
    qualityPassPct:
      qpScores.length > 0
        ? qpScores.reduce((a, b) => a + b, 0) / qpScores.length
        : null,
  };
}

function HeroCard({
  label,
  value,
  hint,
  emerald,
}: {
  label: string;
  value: string;
  hint?: string;
  emerald?: boolean;
}) {
  return (
    <div className="gatewayllm-card flex flex-col gap-1 px-4 py-3">
      <span className="text-[11px] font-medium uppercase tracking-wide text-zinc-500">{label}</span>
      <span
        className={`text-2xl font-semibold tabular-nums ${
          emerald ? 'text-emerald-300' : 'text-white'
        }`}
      >
        {value}
      </span>
      {hint && <span className="text-xs text-zinc-500">{hint}</span>}
    </div>
  );
}

export default function SavingsPage() {
  const [list, setList] = useState<SavingsListResponse>({ data: [] });
  const [daily, setDaily] = useState<DailySavings[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(0);
  const [aliasFilter, setAliasFilter] = useState<string>('');

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        setLoading(true);
        setError(null);
        const [s, d] = await Promise.all([
          getSavings(PAGE_SIZE, page * PAGE_SIZE, aliasFilter || undefined),
          getDailySavings(30),
        ]);
        if (cancelled) return;
        setList({
          data: s.data ?? [],
          alias_breakdown: s.alias_breakdown ?? [],
        });
        setDaily(d ?? []);
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load savings');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [page, aliasFilter]);

  const hero = useMemo(
    () => deriveHeroStats(list.data, list.alias_breakdown),
    [list],
  );

  const chartData = useMemo(
    () =>
      [...daily]
        .sort((a, b) => a.date.localeCompare(b.date))
        .map((d) => ({
          date: d.date.slice(0, 10),
          savings: Number(d.total_savings_usd.toFixed(4)),
          our_cut: Number(d.our_cut_usd.toFixed(4)),
          requests: d.routed_requests,
        })),
    [daily],
  );

  const canPrev = page > 0;
  const canNext = list.data.length === PAGE_SIZE;

  const aliasOptions = useMemo(() => {
    const set = new Set<string>();
    for (const a of list.alias_breakdown ?? []) set.add(a.requested_alias);
    return Array.from(set).sort();
  }, [list.alias_breakdown]);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Savings"
        description="Per-request savings ledger from the smart-routing moat. Every row is signed with the operator's Ed25519 key."
        action={
          <div className="flex items-center gap-2">
            <select
              className="gatewayllm-input py-1.5 text-sm"
              value={aliasFilter}
              onChange={(e) => {
                setAliasFilter(e.target.value);
                setPage(0);
              }}
            >
              <option value="">All aliases</option>
              {aliasOptions.map((a) => (
                <option key={a} value={a}>
                  {a}
                </option>
              ))}
            </select>
          </div>
        }
      />

      {error && <Alert variant="error" onDismiss={() => setError(null)}>{error}</Alert>}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <HeroCard
          label="Total saved"
          value={fmtUSD(hero.savings, 4)}
          hint={`baseline ${fmtUSD(hero.baseline, 2)} → actual ${fmtUSD(hero.actual, 2)}`}
          emerald
        />
        <HeroCard
          label="Requests routed"
          value={fmtPct(hero.routedPct)}
          hint={`${fmtNum(list.data.length)} rows in this page`}
        />
        <HeroCard
          label="Quality pass %"
          value={fmtPct(hero.qualityPassPct ?? undefined)}
          hint="from sampled judge runs"
        />
        <HeroCard
          label="30-day cut"
          value={fmtUSD(daily.reduce((s, d) => s + (d.our_cut_usd ?? 0), 0), 2)}
          hint="our share of savings"
        />
      </div>

      <div className="gatewayllm-card p-4">
        <h2 className="mb-3 text-sm font-semibold text-white">Savings over time</h2>
        {chartData.length === 0 ? (
          <div className="py-12 text-center text-sm text-zinc-500">No daily rollups yet.</div>
        ) : (
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                <defs>
                  <linearGradient id="savingsGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#10b981" stopOpacity={0.5} />
                    <stop offset="100%" stopColor="#10b981" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
                <XAxis dataKey="date" stroke="#71717a" tick={{ fontSize: 11 }} />
                <YAxis
                  stroke="#71717a"
                  tick={{ fontSize: 11 }}
                  tickFormatter={(v) => `$${v}`}
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: '#0a0a0a',
                    border: '1px solid #27272a',
                    borderRadius: 8,
                    fontSize: 12,
                  }}
                  formatter={(v: number) => fmtUSD(v, 4)}
                />
                <Area
                  type="monotone"
                  dataKey="savings"
                  stroke="#10b981"
                  fill="url(#savingsGrad)"
                  strokeWidth={2}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        )}
      </div>

      {list.alias_breakdown && list.alias_breakdown.length > 0 && (
        <div className="gatewayllm-card overflow-hidden">
          <div className="border-b border-zinc-800 bg-zinc-900/40 px-4 py-2 text-xs font-medium uppercase tracking-wide text-zinc-400">
            By alias
          </div>
          <table className="gatewayllm-table text-xs sm:text-sm">
            <thead className="bg-zinc-900/40">
              <tr>
                <th className="gatewayllm-th">Requested</th>
                <th className="gatewayllm-th">Served</th>
                <th className="gatewayllm-th text-right">Requests</th>
                <th className="gatewayllm-th text-right">Routed</th>
                <th className="gatewayllm-th text-right">Baseline $</th>
                <th className="gatewayllm-th text-right">Actual $</th>
                <th className="gatewayllm-th text-right">Saved $</th>
                <th className="gatewayllm-th text-right">Quality</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800">
              {list.alias_breakdown.map((a, i) => (
                <tr key={`${a.requested_alias}-${a.served_alias}-${i}`}>
                  <td className="gatewayllm-td font-mono text-emerald-300/90">{a.requested_alias}</td>
                  <td className="gatewayllm-td font-mono text-zinc-300">{a.served_alias}</td>
                  <td className="gatewayllm-td text-right tabular-nums">{fmtNum(a.requests)}</td>
                  <td className="gatewayllm-td text-right tabular-nums">{fmtNum(a.routed)}</td>
                  <td className="gatewayllm-td text-right tabular-nums">{fmtUSD(a.baseline_usd, 4)}</td>
                  <td className="gatewayllm-td text-right tabular-nums">{fmtUSD(a.actual_usd, 4)}</td>
                  <td className="gatewayllm-td text-right tabular-nums text-emerald-300">
                    {fmtUSD(a.savings_usd, 4)}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums">{fmtPct(a.quality_pass_pct)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="gatewayllm-table-wrap">
        <div className="flex items-center justify-between border-b border-zinc-800 bg-zinc-900/40 px-4 py-2">
          <span className="text-xs font-medium uppercase tracking-wide text-zinc-400">
            Recent routed requests
          </span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              className="gatewayllm-btn-secondary py-1.5 text-xs"
              disabled={!canPrev || loading}
              onClick={() => setPage((p) => Math.max(0, p - 1))}
            >
              Previous
            </button>
            <span className="text-xs text-zinc-500">Page {page + 1}</span>
            <button
              type="button"
              className="gatewayllm-btn-secondary py-1.5 text-xs"
              disabled={!canNext || loading}
              onClick={() => setPage((p) => p + 1)}
            >
              Next
            </button>
          </div>
        </div>
        <table className="gatewayllm-table text-xs sm:text-sm">
          <thead className="bg-zinc-900/40">
            <tr>
              <th className="gatewayllm-th">Time</th>
              <th className="gatewayllm-th">Trace</th>
              <th className="gatewayllm-th">Requested → Served</th>
              <th className="gatewayllm-th">Strategy</th>
              <th className="gatewayllm-th text-right">Baseline $</th>
              <th className="gatewayllm-th text-right">Actual $</th>
              <th className="gatewayllm-th text-right">Saved $</th>
              <th className="gatewayllm-th text-right">Quality</th>
              <th className="gatewayllm-th"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-800 bg-zinc-950/40">
            {loading ? (
              <tr>
                <td colSpan={9} className="gatewayllm-td text-center text-zinc-500">
                  Loading…
                </td>
              </tr>
            ) : list.data.length === 0 ? (
              <tr>
                <td colSpan={9} className="gatewayllm-td text-center text-zinc-500">
                  No routed requests yet — set a routing policy to <code>auto_retry</code> or
                  <code>track_only</code> to start emitting ledger rows.
                </td>
              </tr>
            ) : (
              list.data.map((r) => (
                <tr key={r.id} className="hover:bg-zinc-900/40">
                  <td className="gatewayllm-td whitespace-nowrap text-zinc-500">
                    {new Date(r.created_at).toLocaleString()}
                  </td>
                  <td className="gatewayllm-td font-mono text-[11px] text-zinc-400">
                    {r.trace_id.slice(0, 12)}…
                  </td>
                  <td className="gatewayllm-td font-mono text-zinc-300">
                    <span className="text-emerald-300/90">{r.requested_alias}</span>
                    <span className="px-1 text-zinc-500">→</span>
                    <span>{r.served_alias}</span>
                  </td>
                  <td className="gatewayllm-td">
                    <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-zinc-300">
                      {r.strategy || '—'}
                    </span>
                    {r.retried && (
                      <span className="ml-1 rounded bg-amber-500/10 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-amber-300">
                        retried
                      </span>
                    )}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums text-zinc-300">
                    {fmtUSD(r.baseline_cost_usd, 6)}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums text-zinc-300">
                    {fmtUSD(r.actual_cost_usd, 6)}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums text-emerald-300">
                    {fmtUSD(r.savings_usd, 6)}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums">
                    {r.quality_score != null ? r.quality_score.toFixed(2) : '—'}
                  </td>
                  <td className="gatewayllm-td">
                    <Link
                      href={`/savings/${r.id}`}
                      className="text-xs text-emerald-400 hover:text-emerald-300"
                    >
                      Drill in →
                    </Link>
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
