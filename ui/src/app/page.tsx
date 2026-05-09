'use client';

import Link from 'next/link';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { DashboardChart } from '@/components/DashboardChart';
import { OnboardingWizard } from '@/components/OnboardingWizard';
import { useAuth } from '@/contexts/AuthContext';
import {
  getAuditEvents,
  getCredentials,
  getDailyUsage,
  getDeployments,
  getGatewayMetrics,
  getKeys,
  getUsage,
} from '@/lib/api';
import {
  isUserApiKey,
  type APIKey,
  type AuditEvent,
  type DailySpend,
  type DeploymentRow,
  type GatewayMetrics,
  type ProviderCredential,
  type SpendLog,
} from '@/lib/types';
import { PageHeader, Alert, LoadingSkeleton, CopyButton, StatCard } from '@/components/ui';

const ONBOARDING_DISMISSED_KEY = 'gatewayllm_onboarding_dismissed';
const ONBOARDING_COMPLETED_KEY = 'gatewayllm_onboarding_completed';

function pctChange(curr: number, prev: number): { label: string; direction: 'up' | 'down' | 'flat' } | undefined {
  if (prev <= 0 && curr <= 0) return undefined;
  if (prev <= 0) return { label: 'new this week', direction: 'up' };
  const pct = ((curr - prev) / prev) * 100;
  const abs = Math.abs(pct);
  const direction: 'up' | 'down' | 'flat' = abs < 1 ? 'flat' : pct > 0 ? 'up' : 'down';
  return { label: `${pct >= 0 ? '+' : ''}${pct.toFixed(abs < 10 ? 1 : 0)}% vs prior 7d`, direction };
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return n.toLocaleString();
}

function formatNumber(n: number): string {
  return n.toLocaleString();
}

function formatRelativeTime(iso: string): string {
  try {
    const diff = Date.now() - new Date(iso).getTime();
    const s = Math.floor(diff / 1000);
    if (s < 60) return `${s}s ago`;
    const m = Math.floor(s / 60);
    if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h ago`;
    const d = Math.floor(h / 24);
    return `${d}d ago`;
  } catch {
    return iso;
  }
}

// percentile returns the p-th percentile (0..100) of a numeric array using
// linear interpolation between adjacent ranks. Returns 0 for empty input.
function percentile(values: number[], p: number): number {
  if (values.length === 0) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  if (sorted.length === 1) return sorted[0];
  const rank = (p / 100) * (sorted.length - 1);
  const lo = Math.floor(rank);
  const hi = Math.ceil(rank);
  if (lo === hi) return sorted[lo];
  return sorted[lo] + (sorted[hi] - sorted[lo]) * (rank - lo);
}

const CACHE_HIT_ENDPOINTS = new Set(['chat:cache_hit', 'chat:sem_hit']);

export default function DashboardPage() {
  const { isAdmin, isTeamAdmin } = useAuth();
  const [daily, setDaily] = useState<DailySpend[]>([]);
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [deployments, setDeployments] = useState<DeploymentRow[]>([]);
  const [credentials, setCredentials] = useState<ProviderCredential[]>([]);
  const [recentLogs, setRecentLogs] = useState<SpendLog[]>([]);
  const [metrics, setMetrics] = useState<GatewayMetrics | null>(null);
  const [auditEvents, setAuditEvents] = useState<AuditEvent[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [wizardDismissed, setWizardDismissed] = useState(false);
  const [onboardingCompleted, setOnboardingCompleted] = useState(false);

  const refresh = useCallback(async () => {
    const [k, depRows, creds, usageDays, logs, m, audit] = await Promise.all([
      getKeys(),
      getDeployments(),
      getCredentials().catch(() => [] as ProviderCredential[]),
      getDailyUsage(30),
      getUsage(100).catch(() => [] as SpendLog[]),
      getGatewayMetrics(7).catch(() => null),
      // /v1/audit is org-admin only; non-admins silently get an empty feed.
      isTeamAdmin ? getAuditEvents(10).catch(() => [] as AuditEvent[]) : Promise.resolve([] as AuditEvent[]),
    ]);
    setKeys(k);
    setDeployments(depRows);
    setCredentials(creds);
    setDaily(usageDays);
    setRecentLogs(logs);
    setMetrics(m);
    setAuditEvents(audit);
  }, [isTeamAdmin]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        setError(null);
        await refresh();
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load dashboard');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    if (typeof window !== 'undefined') {
      setWizardDismissed(localStorage.getItem(ONBOARDING_DISMISSED_KEY) === 'true');
      setOnboardingCompleted(localStorage.getItem(ONBOARDING_COMPLETED_KEY) === 'true');
    }
    return () => {
      cancelled = true;
    };
  }, [refresh]);

  function dismissWizard() {
    if (typeof window !== 'undefined') {
      localStorage.setItem(ONBOARDING_DISMISSED_KEY, 'true');
      localStorage.removeItem('gatewayllm_onboarding_pending');
    }
    setWizardDismissed(true);
  }

  function completeOnboarding() {
    if (typeof window !== 'undefined') {
      localStorage.setItem(ONBOARDING_COMPLETED_KEY, 'true');
      localStorage.setItem(ONBOARDING_DISMISSED_KEY, 'true');
      localStorage.removeItem('gatewayllm_onboarding_pending');
    }
    setOnboardingCompleted(true);
    setWizardDismissed(true);
  }

  function resumeWizard() {
    if (typeof window !== 'undefined') {
      localStorage.removeItem(ONBOARDING_DISMISSED_KEY);
    }
    setWizardDismissed(false);
  }

  // Derived stats
  const totals = useMemo(() => {
    const totalSpend = daily.reduce((s, d) => s + d.total_cost_usd, 0);
    const totalReq = daily.reduce((s, d) => s + d.request_count, 0);
    const totalTokens = daily.reduce((s, d) => s + d.total_tokens, 0);
    return { totalSpend, totalReq, totalTokens };
  }, [daily]);

  const deltas = useMemo(() => {
    if (daily.length === 0) return {} as Record<string, ReturnType<typeof pctChange>>;
    const sorted = [...daily].sort((a, b) => new Date(a.date).getTime() - new Date(b.date).getTime());
    const last7 = sorted.slice(-7);
    const prev7 = sorted.slice(-14, -7);
    const sumReq = (rows: DailySpend[]) => rows.reduce((s, d) => s + d.request_count, 0);
    const sumSpend = (rows: DailySpend[]) => rows.reduce((s, d) => s + d.total_cost_usd, 0);
    const sumTok = (rows: DailySpend[]) => rows.reduce((s, d) => s + d.total_tokens, 0);
    return {
      requests: pctChange(sumReq(last7), sumReq(prev7)),
      spend: pctChange(sumSpend(last7), sumSpend(prev7)),
      tokens: pctChange(sumTok(last7), sumTok(prev7)),
    };
  }, [daily]);

  const logStats = useMemo(() => {
    const empty = {
      p50LatencyMs: 0,
      p95LatencyMs: 0,
      p99LatencyMs: 0,
      clientErrorRate: 0,
      serverErrorRate: 0,
      cacheHitRate: 0,
      cacheHitCount: 0,
      providerMix: [] as [string, number][],
      byModel: [] as [string, { spend: number; req: number }][],
      byKey: [] as [string, { spend: number; req: number }][],
    };
    if (recentLogs.length === 0) return empty;

    const latencies = recentLogs
      .map((l) => l.latency_ms)
      .filter((v): v is number => typeof v === 'number' && v >= 0);
    const p50 = percentile(latencies, 50);
    const p95 = percentile(latencies, 95);
    const p99 = percentile(latencies, 99);

    const total = recentLogs.length;
    const clientErrors = recentLogs.filter((l) => {
      const c = l.status_code ?? 200;
      return c >= 400 && c < 500;
    }).length;
    const serverErrors = recentLogs.filter((l) => (l.status_code ?? 200) >= 500).length;
    const cacheHits = recentLogs.filter((l) => CACHE_HIT_ENDPOINTS.has(l.endpoint)).length;

    const modelMap = new Map<string, { spend: number; req: number }>();
    const keyMap = new Map<string, { spend: number; req: number }>();
    const providerMap = new Map<string, number>();
    for (const l of recentLogs) {
      const m = modelMap.get(l.model_alias) ?? { spend: 0, req: 0 };
      m.spend += l.cost_usd ?? 0;
      m.req += 1;
      modelMap.set(l.model_alias, m);

      const kid = l.api_key_id ?? 'unknown';
      const k = keyMap.get(kid) ?? { spend: 0, req: 0 };
      k.spend += l.cost_usd ?? 0;
      k.req += 1;
      keyMap.set(kid, k);

      const p = l.provider || 'unknown';
      providerMap.set(p, (providerMap.get(p) ?? 0) + 1);
    }

    return {
      p50LatencyMs: p50,
      p95LatencyMs: p95,
      p99LatencyMs: p99,
      clientErrorRate: (clientErrors / total) * 100,
      serverErrorRate: (serverErrors / total) * 100,
      cacheHitRate: (cacheHits / total) * 100,
      cacheHitCount: cacheHits,
      providerMix: Array.from(providerMap.entries()).sort((a, b) => b[1] - a[1]),
      byModel: Array.from(modelMap.entries()).sort((a, b) => b[1].spend - a[1].spend).slice(0, 3),
      byKey: Array.from(keyMap.entries()).sort((a, b) => b[1].spend - a[1].spend).slice(0, 3),
    };
  }, [recentLogs]);

  const keyNameById = useMemo(() => {
    const m = new Map<string, string>();
    for (const k of keys) m.set(k.id, k.name);
    return m;
  }, [keys]);

  // Exclude the auto-provisioned `session:<email>` key so the onboarding
  // progress reflects real gateway keys the user has created.
  const realKeys = useMemo(() => keys.filter(isUserApiKey), [keys]);

  // Keys with a configured budget cap, sorted by burn ratio descending.
  const budgetRows = useMemo(() => {
    return realKeys
      .filter((k) => typeof k.max_budget === 'number' && (k.max_budget as number) > 0)
      .map((k) => {
        const max = (k.max_budget as number) || 0;
        const spent = k.total_spend ?? 0;
        const ratio = max > 0 ? spent / max : 0;
        return { key: k, spent, max, ratio };
      })
      .sort((a, b) => b.ratio - a.ratio)
      .slice(0, 5);
  }, [realKeys]);

  // Pick the most-sampled feedback metric as the dashboard "Quality" headline.
  const quality = useMemo(() => {
    if (!metrics || metrics.data.length === 0) return null;
    const numeric = metrics.data.filter(
      (m) => typeof m.avg_float === 'number' && typeof m.count === 'number' && m.count > 0,
    );
    if (numeric.length === 0) return null;
    const top = [...numeric].sort((a, b) => (b.count ?? 0) - (a.count ?? 0))[0];
    return top;
  }, [metrics]);

  const needsSetup =
    credentials.length === 0 ||
    deployments.length === 0 ||
    realKeys.length === 0 ||
    (totals.totalReq === 0 && !onboardingCompleted);
  const pendingFromSignup =
    typeof window !== 'undefined' && localStorage.getItem('gatewayllm_onboarding_pending') === 'true';
  // Wizard only appears while there is real setup left to do. Signup flag can
  // override "dismissed" but never overrides "done".
  const showWizard = !loading && needsSetup && (pendingFromSignup || !wizardDismissed);

  // Auto-clear the signup-pending flag once the user has completed all four
  // onboarding steps. Otherwise the wizard would keep re-appearing on every
  // refresh until the user explicitly clicks "Skip" / "I'm all set".
  useEffect(() => {
    if (loading || needsSetup) return;
    if (typeof window === 'undefined') return;
    localStorage.removeItem('gatewayllm_onboarding_pending');
  }, [loading, needsSetup]);

  const activeKeys = realKeys.filter((k) => k.is_active).length;
  const modelCount = new Set(deployments.map((d) => d.model_alias)).size;

  // Completed setup steps (out of 4: provider, route, key, first request)
  const completedSteps =
    (credentials.length > 0 ? 1 : 0) +
    (deployments.length > 0 ? 1 : 0) +
    (realKeys.length > 0 ? 1 : 0) +
    (totals.totalReq > 0 || onboardingCompleted ? 1 : 0);

  const nextStep = useMemo(() => {
    if (credentials.length === 0)
      return { label: 'Add your first provider key', href: '/credentials' };
    if (deployments.length === 0)
      return { label: 'Create your first model route', href: '/models' };
    if (realKeys.length === 0) return { label: 'Generate your gateway API key', href: '/keys' };
    if (totals.totalReq === 0) return { label: 'Send your first request', href: '/keys' };
    return null;
  }, [credentials.length, deployments.length, realKeys.length, totals.totalReq]);

  const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
  const endpointUrl = `${apiUrl}/v1`;

  return (
    <div className="space-y-8">
      <PageHeader title="Dashboard" description="Overview of gateway usage and configuration." />

      {error && <Alert variant="warning">{error}</Alert>}

      {/* Endpoint card */}
      <div className="gatewayllm-card p-5">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <p className="text-xs font-medium uppercase tracking-wide text-zinc-500">Your gateway endpoint</p>
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <code className="rounded-md border border-zinc-800 bg-zinc-950 px-3 py-1.5 font-mono text-sm text-emerald-200">
                {endpointUrl}
              </code>
              <CopyButton value={endpointUrl} label="Copy endpoint" />
            </div>
            <p className="mt-2 text-xs text-zinc-500">
              OpenAI-compatible. Point any OpenAI SDK at this URL with your Gateway API key as the bearer token.
            </p>
          </div>
          <Link href="/keys" className="gatewayllm-btn-secondary text-xs">
            Manage API keys
          </Link>
        </div>
      </div>

      {showWizard && (
        <OnboardingWizard
          credentials={credentials}
          deployments={deployments}
          keys={realKeys}
          totalReq={totals.totalReq}
          onRefresh={refresh}
          onDismiss={dismissWizard}
          onComplete={completeOnboarding}
        />
      )}

      {!showWizard && needsSetup && nextStep && (
        <Alert variant="info">
          <span className="mr-2">Setup is incomplete ({completedSteps}/4).</span>
          <Link href={nextStep.href} className="font-medium underline underline-offset-2">
            {nextStep.label}
          </Link>
          <button
            type="button"
            className="ml-3 text-xs text-zinc-400 underline underline-offset-2"
            onClick={resumeWizard}
          >
            Resume onboarding
          </button>
        </Alert>
      )}

      {loading ? (
        <LoadingSkeleton rows={4} header={false} />
      ) : (
        <>
          {/* Volume row */}
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
            <StatCard
              title="Requests (30d)"
              value={formatNumber(totals.totalReq)}
              delta={deltas.requests}
            />
            <StatCard
              title="Spend (30d)"
              value={`$${totals.totalSpend.toFixed(4)}`}
              hint="From daily aggregates"
              delta={deltas.spend}
            />
            <StatCard
              title="Tokens (30d)"
              value={formatTokens(totals.totalTokens)}
              hint="Prompt + completion"
              delta={deltas.tokens}
            />
            <StatCard
              title="Latency p50 / p95"
              value={
                logStats.p95LatencyMs > 0
                  ? `${Math.round(logStats.p50LatencyMs)} / ${Math.round(logStats.p95LatencyMs)} ms`
                  : '—'
              }
              hint={
                logStats.p99LatencyMs > 0
                  ? `p99 ${Math.round(logStats.p99LatencyMs)} ms (last 100)`
                  : 'Over last 100 requests'
              }
            />
            <StatCard
              title="Errors 4xx / 5xx"
              value={
                recentLogs.length > 0
                  ? `${logStats.clientErrorRate.toFixed(1)}% / ${logStats.serverErrorRate.toFixed(1)}%`
                  : '—'
              }
              hint="Client / gateway errors over last 100"
              delta={
                recentLogs.length > 0
                  ? {
                      label:
                        logStats.serverErrorRate < 1
                          ? 'healthy'
                          : logStats.serverErrorRate < 5
                          ? 'watching'
                          : 'high',
                      direction:
                        logStats.serverErrorRate < 1
                          ? 'up'
                          : logStats.serverErrorRate < 5
                          ? 'flat'
                          : 'down',
                    }
                  : undefined
              }
            />
            <StatCard
              title="Cache hit rate"
              value={recentLogs.length > 0 ? `${logStats.cacheHitRate.toFixed(1)}%` : '—'}
              hint={
                recentLogs.length > 0
                  ? `${logStats.cacheHitCount} of last ${recentLogs.length} served from cache`
                  : 'Exact + semantic, last 100'
              }
            />
          </div>

          {/* Quality row */}
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <SmartRouteCard metrics={metrics} />
            <QualityCard quality={quality} />
            <StatCard
              title="Keys / Models"
              value={`${activeKeys} / ${modelCount}`}
              hint={`${activeKeys} active key${activeKeys === 1 ? '' : 's'}, ${modelCount} alias${modelCount === 1 ? '' : 'es'}`}
            />
          </div>
        </>
      )}

      <div className="gatewayllm-card p-6">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h2 className="text-lg font-medium text-white">Spend &amp; requests over time</h2>
            <p className="text-sm text-zinc-500">Daily cost (left axis) and request volume (right, last 30 days)</p>
          </div>
        </div>
        <DashboardChart data={daily} />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <div className="gatewayllm-card p-6">
          <div className="mb-4 flex items-center justify-between">
            <div>
              <h2 className="text-lg font-medium text-white">Top models</h2>
              <p className="text-sm text-zinc-500">By spend, last 100 requests</p>
            </div>
          </div>
          {logStats.byModel.length === 0 ? (
            <p className="text-sm text-zinc-500">No traffic yet.</p>
          ) : (
            <ul className="space-y-2">
              {logStats.byModel.map(([model, v], i) => (
                <li key={model} className="flex items-center justify-between rounded-lg border border-zinc-800/70 bg-zinc-900/40 px-3 py-2">
                  <span className="flex min-w-0 items-center gap-3">
                    <span className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-emerald-500/15 text-xs font-medium text-emerald-300">
                      {i + 1}
                    </span>
                    <span className="truncate font-mono text-sm text-zinc-200" title={model}>{model}</span>
                  </span>
                  <span className="flex shrink-0 items-center gap-3 text-right text-xs text-zinc-400">
                    <span className="tabular-nums">{formatNumber(v.req)} req</span>
                    <span className="tabular-nums text-zinc-200">${v.spend.toFixed(4)}</span>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>

        <ProviderMixCard providerMix={logStats.providerMix} totalLogs={recentLogs.length} />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <BudgetCard rows={budgetRows} />
        {isTeamAdmin ? (
          <ActivityCard events={auditEvents} />
        ) : (
          <div className="gatewayllm-card p-6">
            <div className="mb-4">
              <h2 className="text-lg font-medium text-white">Top keys</h2>
              <p className="text-sm text-zinc-500">By spend, last 100 requests</p>
            </div>
            {logStats.byKey.length === 0 ? (
              <p className="text-sm text-zinc-500">No traffic yet.</p>
            ) : (
              <ul className="space-y-2">
                {logStats.byKey.map(([kid, v], i) => {
                  const name = kid === 'unknown' ? 'unattributed' : keyNameById.get(kid) ?? kid.slice(0, 8);
                  return (
                    <li key={kid} className="flex items-center justify-between rounded-lg border border-zinc-800/70 bg-zinc-900/40 px-3 py-2">
                      <span className="flex min-w-0 items-center gap-3">
                        <span className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-emerald-500/15 text-xs font-medium text-emerald-300">
                          {i + 1}
                        </span>
                        <span className="truncate text-sm text-zinc-200" title={name}>{name}</span>
                      </span>
                      <span className="flex shrink-0 items-center gap-3 text-right text-xs text-zinc-400">
                        <span className="tabular-nums">{formatNumber(v.req)} req</span>
                        <span className="tabular-nums text-zinc-200">${v.spend.toFixed(4)}</span>
                      </span>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        )}
      </div>

      <div className="gatewayllm-card p-6">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h2 className="text-lg font-medium text-white">Recent requests</h2>
            <p className="text-sm text-zinc-500">Last 5 gateway requests</p>
          </div>
          <Link href="/usage" className="text-xs text-emerald-400 hover:text-emerald-300">
            View all &rarr;
          </Link>
        </div>
        {recentLogs.length === 0 ? (
          <p className="text-sm text-zinc-500">Nothing here yet. Send a request to your endpoint to see it land.</p>
        ) : (
          <div className="gatewayllm-table-wrap">
            <table className="gatewayllm-table">
              <thead className="bg-zinc-900/50">
                <tr>
                  <th className="gatewayllm-th">Model</th>
                  <th className="gatewayllm-th">Tokens</th>
                  <th className="gatewayllm-th">Cost</th>
                  <th className="gatewayllm-th">Status</th>
                  <th className="gatewayllm-th">Latency</th>
                  <th className="gatewayllm-th">When</th>
                </tr>
              </thead>
              <tbody>
                {recentLogs.slice(0, 5).map((l) => {
                  const ok = (l.status_code ?? 200) < 400;
                  return (
                    <tr key={l.id} className="border-t border-zinc-800/60">
                      <td className="gatewayllm-td font-mono text-xs">{l.model_alias}</td>
                      <td className="gatewayllm-td tabular-nums text-xs">{formatTokens(l.total_tokens ?? 0)}</td>
                      <td className="gatewayllm-td tabular-nums text-xs">${(l.cost_usd ?? 0).toFixed(4)}</td>
                      <td className="gatewayllm-td">
                        <span
                          className={`inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium ${
                            ok
                              ? 'bg-emerald-500/10 text-emerald-300 ring-1 ring-emerald-700/40'
                              : 'bg-red-500/10 text-red-300 ring-1 ring-red-700/40'
                          }`}
                        >
                          {l.status_code ?? 200}
                        </span>
                      </td>
                      <td className="gatewayllm-td tabular-nums text-xs">{l.latency_ms ?? 0} ms</td>
                      <td className="gatewayllm-td text-xs text-zinc-400">{formatRelativeTime(l.created_at)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {isAdmin && (
        <div className="gatewayllm-card p-6">
          <div className="mb-3 flex items-center justify-between">
            <div>
              <h2 className="text-lg font-medium text-white">Top keys</h2>
              <p className="text-sm text-zinc-500">By spend, last 100 requests</p>
            </div>
          </div>
          {logStats.byKey.length === 0 ? (
            <p className="text-sm text-zinc-500">No traffic yet.</p>
          ) : (
            <ul className="space-y-2">
              {logStats.byKey.map(([kid, v], i) => {
                const name = kid === 'unknown' ? 'unattributed' : keyNameById.get(kid) ?? kid.slice(0, 8);
                return (
                  <li key={kid} className="flex items-center justify-between rounded-lg border border-zinc-800/70 bg-zinc-900/40 px-3 py-2">
                    <span className="flex min-w-0 items-center gap-3">
                      <span className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-emerald-500/15 text-xs font-medium text-emerald-300">
                        {i + 1}
                      </span>
                      <span className="truncate text-sm text-zinc-200" title={name}>{name}</span>
                    </span>
                    <span className="flex shrink-0 items-center gap-3 text-right text-xs text-zinc-400">
                      <span className="tabular-nums">{formatNumber(v.req)} req</span>
                      <span className="tabular-nums text-zinc-200">${v.spend.toFixed(4)}</span>
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}

// ---------- Sub-components ----------

function SmartRouteCard({ metrics }: { metrics: GatewayMetrics | null }) {
  const sr = metrics?.smart_route;
  const sc = metrics?.semantic_cache;
  const srEnabled = !!sr?.enabled;
  const scEnabled = !!sc?.enabled;
  const decisions = sr?.total_decisions ?? 0;
  const overrides = sr?.tier_overrides ?? {};
  const topOverride = Object.entries(overrides).sort((a, b) => b[1] - a[1])[0];
  const semHits = sc?.hits ?? 0;
  const semMisses = sc?.misses ?? 0;
  const semTotal = semHits + semMisses;
  const semHitRate = semTotal > 0 ? (semHits / semTotal) * 100 : 0;
  const shadow = sr?.shadow;
  const shadowTotal = (shadow?.shadow_wins ?? 0) + (shadow?.primary_wins ?? 0);
  const shadowWinRate = shadowTotal > 0 ? ((shadow?.shadow_wins ?? 0) / shadowTotal) * 100 : 0;

  return (
    <div className="gatewayllm-card p-5">
      <p className="text-xs font-medium uppercase tracking-wide text-zinc-500">Routing &amp; cache intelligence</p>
      <div className="mt-3 space-y-2 text-sm">
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-zinc-400">SmartRoute</span>
          {srEnabled ? (
            <span className="text-zinc-200 tabular-nums">
              {formatNumber(decisions)} decisions
              {topOverride ? <span className="ml-1 text-xs text-zinc-500">({topOverride[0]} ×{topOverride[1]})</span> : null}
            </span>
          ) : (
            <span className="text-zinc-500">disabled</span>
          )}
        </div>
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-zinc-400">Semantic cache</span>
          {scEnabled ? (
            <span className="text-zinc-200 tabular-nums">
              {semHitRate.toFixed(1)}%{' '}
              <span className="text-xs text-zinc-500">({formatNumber(semHits)}/{formatNumber(semTotal)})</span>
            </span>
          ) : (
            <span className="text-zinc-500">disabled</span>
          )}
        </div>
        {shadow && shadowTotal > 0 && (
          <div className="flex items-baseline justify-between gap-3">
            <span className="text-zinc-400">Shadow win rate</span>
            <span className="text-zinc-200 tabular-nums">
              {shadowWinRate.toFixed(0)}%{' '}
              <span className="text-xs text-zinc-500">({formatNumber(shadow?.shadow_wins ?? 0)}/{formatNumber(shadowTotal)})</span>
            </span>
          </div>
        )}
        {!srEnabled && !scEnabled && (
          <p className="text-xs text-zinc-500">Enable smart_route or semantic_cache in config to populate this card.</p>
        )}
      </div>
    </div>
  );
}

function QualityCard({
  quality,
}: {
  quality: { metric: string; model_alias: string; avg_float?: number | null; count: number } | null;
}) {
  if (!quality) {
    return (
      <StatCard
        title="Quality"
        value="—"
        hint="POST /v1/feedback to track quality scores"
      />
    );
  }
  const score = quality.avg_float ?? 0;
  return (
    <StatCard
      title={`Quality · ${quality.metric}`}
      value={score.toFixed(2)}
      hint={`avg over ${formatNumber(quality.count)} signal${quality.count === 1 ? '' : 's'} for ${quality.model_alias}`}
    />
  );
}

function ProviderMixCard({ providerMix, totalLogs }: { providerMix: [string, number][]; totalLogs: number }) {
  return (
    <div className="gatewayllm-card p-6">
      <div className="mb-4">
        <h2 className="text-lg font-medium text-white">Provider mix</h2>
        <p className="text-sm text-zinc-500">Request share by provider, last 100 requests</p>
      </div>
      {providerMix.length === 0 || totalLogs === 0 ? (
        <p className="text-sm text-zinc-500">No traffic yet.</p>
      ) : (
        <ul className="space-y-2">
          {providerMix.map(([provider, count]) => {
            const pct = (count / totalLogs) * 100;
            return (
              <li key={provider} className="space-y-1">
                <div className="flex items-baseline justify-between text-xs">
                  <span className="font-mono text-zinc-200">{provider}</span>
                  <span className="tabular-nums text-zinc-400">
                    {pct.toFixed(0)}% · {formatNumber(count)} req
                  </span>
                </div>
                <div className="h-1.5 overflow-hidden rounded-full bg-zinc-800">
                  <div
                    className="h-full bg-emerald-500/70"
                    style={{ width: `${Math.max(2, Math.min(100, pct))}%` }}
                  />
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function BudgetCard({
  rows,
}: {
  rows: { key: APIKey; spent: number; max: number; ratio: number }[];
}) {
  return (
    <div className="gatewayllm-card p-6">
      <div className="mb-4 flex items-center justify-between">
        <div>
          <h2 className="text-lg font-medium text-white">Budget consumption</h2>
          <p className="text-sm text-zinc-500">Top keys with a budget cap configured</p>
        </div>
        <Link href="/keys" className="text-xs text-emerald-400 hover:text-emerald-300">
          Manage &rarr;
        </Link>
      </div>
      {rows.length === 0 ? (
        <p className="text-sm text-zinc-500">
          No keys have a budget set. Add <code className="text-zinc-300">max_budget</code> on a key to track spend caps.
        </p>
      ) : (
        <ul className="space-y-3">
          {rows.map(({ key, spent, max, ratio }) => {
            const pct = Math.min(100, ratio * 100);
            const tone =
              ratio >= 0.9
                ? 'bg-red-500/80'
                : ratio >= 0.7
                ? 'bg-amber-500/80'
                : 'bg-emerald-500/70';
            return (
              <li key={key.id} className="space-y-1">
                <div className="flex items-baseline justify-between text-sm">
                  <span className="truncate text-zinc-200" title={key.name}>{key.name}</span>
                  <span className="tabular-nums text-xs text-zinc-400">
                    ${spent.toFixed(2)} / ${max.toFixed(2)}{' '}
                    <span className={ratio >= 0.9 ? 'text-red-300' : 'text-zinc-500'}>
                      ({pct.toFixed(0)}%)
                    </span>
                  </span>
                </div>
                <div className="h-1.5 overflow-hidden rounded-full bg-zinc-800">
                  <div className={`h-full ${tone}`} style={{ width: `${Math.max(2, pct)}%` }} />
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function ActivityCard({ events }: { events: AuditEvent[] }) {
  return (
    <div className="gatewayllm-card p-6">
      <div className="mb-4">
        <h2 className="text-lg font-medium text-white">Recent activity</h2>
        <p className="text-sm text-zinc-500">Audit log entries for this organization</p>
      </div>
      {events.length === 0 ? (
        <p className="text-sm text-zinc-500">No audit events yet.</p>
      ) : (
        <ul className="space-y-2">
          {events.slice(0, 8).map((ev) => (
            <li
              key={ev.id}
              className="flex items-start justify-between gap-3 rounded-lg border border-zinc-800/70 bg-zinc-900/40 px-3 py-2"
            >
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm text-zinc-200">
                  <span className="font-mono text-emerald-300">{ev.action}</span>
                  {ev.resource ? <span className="text-zinc-400"> · {ev.resource}</span> : null}
                </p>
                <p className="truncate text-xs text-zinc-500">
                  {ev.actor_type}
                  {ev.actor_id ? ` ${ev.actor_id.slice(0, 8)}` : ''}
                  {ev.ip ? ` · ${ev.ip}` : ''}
                </p>
              </div>
              <span className="shrink-0 text-xs text-zinc-500">{formatRelativeTime(ev.occurred_at)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
