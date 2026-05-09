'use client';

import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useState } from 'react';
import { loginUser } from '@/lib/api';

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [adminKey, setAdminKey] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onUserSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    if (!email.trim() || !password) {
      setErr('Email and password are required.');
      return;
    }
    setLoading(true);
    try {
      const res = await loginUser(email.trim(), password);
      localStorage.removeItem('gatewayllm_master_key');
      localStorage.setItem('gatewayllm_token', res.token);
      localStorage.setItem('gatewayllm_user', JSON.stringify(res.user));
      router.push('/');
      router.refresh();
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Login failed');
    } finally {
      setLoading(false);
    }
  }

  async function onAdminSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    const trimmed = adminKey.trim();
    if (!trimmed) {
      setErr('Enter the Gateway admin key.');
      return;
    }
    localStorage.removeItem('gatewayllm_token');
    localStorage.removeItem('gatewayllm_user');
    localStorage.setItem('gatewayllm_master_key', trimmed);
    router.push('/');
    router.refresh();
  }

  const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center overflow-hidden bg-zinc-950 px-4 py-10">
      {/* Animated emerald hero background */}
      <div aria-hidden className="pointer-events-none absolute inset-0 overflow-hidden">
        <div
          className="gatewayllm-orb absolute -left-32 top-[-10%] h-[520px] w-[520px] rounded-full blur-3xl"
          style={{
            background:
              'radial-gradient(circle at center, rgba(16, 185, 129, 0.45) 0%, rgba(16, 185, 129, 0.12) 45%, transparent 70%)',
          }}
        />
        <div
          className="gatewayllm-orb-alt absolute -right-24 bottom-[-15%] h-[600px] w-[600px] rounded-full blur-3xl"
          style={{
            background:
              'radial-gradient(circle at center, rgba(5, 150, 105, 0.35) 0%, rgba(6, 78, 59, 0.18) 45%, transparent 70%)',
          }}
        />
        <div
          className="gatewayllm-grid-drift absolute inset-0 opacity-[0.07]"
          style={{
            backgroundImage:
              'radial-gradient(rgba(16, 185, 129, 0.7) 1px, transparent 1px)',
            backgroundSize: '48px 48px',
          }}
        />
        <div className="absolute inset-0 bg-gradient-to-b from-zinc-950/40 via-zinc-950/10 to-zinc-950/80" />
      </div>

      <div className="relative z-10 grid w-full max-w-5xl gap-10 lg:grid-cols-[1.1fr_minmax(0,1fr)] lg:items-center">
        {/* Left: brand + value props */}
        <div className="hidden space-y-8 lg:block">
          <div className="inline-flex items-center gap-2 rounded-full border border-emerald-500/30 bg-emerald-500/5 px-3 py-1 text-xs text-emerald-300">
            <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-400" />
            Production-ready gateway
          </div>
          <h1 className="text-4xl font-semibold leading-tight tracking-tight text-white">
            Gateway<span className="text-emerald-400">-LLM</span>
            <br />
            <span className="text-zinc-300">One endpoint. Every model.</span>
          </h1>
          <p className="max-w-md text-sm text-zinc-400">
            Drop-in OpenAI-compatible gateway that routes to OpenAI, Anthropic, and Gemini with
            per-key quotas, live observability, and sub-10&micro;s routing overhead.
          </p>
          <ul className="space-y-3 text-sm text-zinc-300">
            <li className="flex items-start gap-3">
              <span className="mt-0.5 inline-flex h-5 w-5 flex-none items-center justify-center rounded-full bg-emerald-500/10 text-emerald-400">&#10003;</span>
              <span><span className="font-medium text-white">Bring your own keys.</span> Your OpenAI / Anthropic / Gemini credentials stay yours, encrypted at rest.</span>
            </li>
            <li className="flex items-start gap-3">
              <span className="mt-0.5 inline-flex h-5 w-5 flex-none items-center justify-center rounded-full bg-emerald-500/10 text-emerald-400">&#10003;</span>
              <span><span className="font-medium text-white">Route to any provider.</span> Virtual model aliases, weighted fallbacks, per-route spend caps.</span>
            </li>
            <li className="flex items-start gap-3">
              <span className="mt-0.5 inline-flex h-5 w-5 flex-none items-center justify-center rounded-full bg-emerald-500/10 text-emerald-400">&#10003;</span>
              <span><span className="font-medium text-white">Sub-10&micro;s overhead.</span> Benchmarked Go core, Redis-backed rate limiting, zero-copy streaming.</span>
            </li>
          </ul>
        </div>

        {/* Right: auth card */}
        <div className="w-full">
          <div className="w-full gatewayllm-card border-zinc-800/80 bg-zinc-900/70 p-8 backdrop-blur-xl">
            <div className="lg:hidden">
              <h1 className="text-center text-xl font-semibold text-white">
                Gateway<span className="text-emerald-400">-LLM</span>
              </h1>
            </div>
            <h2 className="mt-1 text-center text-lg font-semibold text-white lg:text-left">Welcome back</h2>
            <p className="mt-1 text-center text-sm text-zinc-400 lg:text-left">
              Sign in to your account to manage keys and routes.
            </p>

            <form onSubmit={onUserSubmit} className="mt-6 space-y-4">
              <div>
                <label htmlFor="email" className="mb-1.5 block text-xs font-medium uppercase tracking-wide text-zinc-500">
                  Email
                </label>
                <input
                  id="email"
                  type="email"
                  autoComplete="email"
                  className="gatewayllm-input text-sm"
                  placeholder="you@company.com"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </div>
              <div>
                <label htmlFor="pw" className="mb-1.5 block text-xs font-medium uppercase tracking-wide text-zinc-500">
                  Password
                </label>
                <input
                  id="pw"
                  type="password"
                  autoComplete="current-password"
                  className="gatewayllm-input text-sm"
                  placeholder="Password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
              {err && !showAdvanced ? <p className="text-sm text-red-400">{err}</p> : null}
              <button type="submit" className="gatewayllm-btn w-full" disabled={loading}>
                {loading ? 'Signing in...' : 'Sign in'}
              </button>
            </form>

            <p className="mt-6 text-center text-sm text-zinc-400">
              New here?{' '}
              <Link href="/signup" className="text-emerald-400 hover:text-emerald-300">
                Create a free account
              </Link>
            </p>

            <div className="mt-6 border-t border-zinc-800/80 pt-4">
              <button
                type="button"
                onClick={() => {
                  setShowAdvanced((v) => !v);
                  setErr(null);
                }}
                aria-expanded={showAdvanced}
                className="flex w-full items-center justify-between text-left text-xs font-medium uppercase tracking-wide text-zinc-500 transition-colors hover:text-zinc-300"
              >
                <span>Advanced: sign in with admin key</span>
                <span aria-hidden className={`transition-transform ${showAdvanced ? 'rotate-180' : ''}`}>&#9662;</span>
              </button>

              {showAdvanced ? (
                <form onSubmit={onAdminSubmit} className="mt-4 space-y-3">
                  <div>
                    <label htmlFor="adminKey" className="mb-1.5 block text-xs font-medium uppercase tracking-wide text-zinc-500">
                      Gateway admin key
                    </label>
                    <input
                      id="adminKey"
                      type="password"
                      autoComplete="off"
                      className="gatewayllm-input font-mono text-sm"
                      placeholder="sk-..."
                      value={adminKey}
                      onChange={(e) => setAdminKey(e.target.value)}
                    />
                    <p className="mt-1 text-xs text-zinc-500">
                      Used by operators to access every org. Most users should sign in with email above.
                    </p>
                  </div>
                  {err && showAdvanced ? <p className="text-sm text-red-400">{err}</p> : null}
                  <button type="submit" className="gatewayllm-btn-secondary w-full">
                    Continue as admin
                  </button>
                </form>
              ) : null}
            </div>

            <p className="mt-6 text-center text-xs text-zinc-500">
              API: <code className="text-zinc-400">{apiUrl}</code>
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
