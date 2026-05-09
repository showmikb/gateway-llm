'use client';

import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { Suspense, useState } from 'react';
import { registerUser } from '@/lib/api';

// Next.js 14 requires any component that calls useSearchParams() during
// rendering to sit inside a <Suspense> boundary so the static prerender can
// bail out gracefully. We wrap the form in an outer Suspense shell to satisfy
// that without adding a loading flash in practice (the form is tiny).
export default function SignupPage() {
  return (
    <Suspense fallback={<SignupShell />}>
      <SignupForm />
    </Suspense>
  );
}

function SignupShell() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-zinc-950 px-4">
      <div className="w-full max-w-md gatewayllm-card p-8">
        <p className="text-center text-sm text-zinc-400">Loading...</p>
      </div>
    </div>
  );
}

function SignupForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const prefilledEmail = searchParams?.get('email') || '';
  const [email, setEmail] = useState(prefilledEmail);
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [name, setName] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);

    const cleanEmail = email.trim().toLowerCase();
    if (!cleanEmail || !cleanEmail.includes('@')) {
      setErr('Enter a valid email address.');
      return;
    }
    if (password.length < 8) {
      setErr('Password must be at least 8 characters.');
      return;
    }
    if (password !== confirm) {
      setErr('Passwords do not match.');
      return;
    }

    setLoading(true);
    try {
      const res = await registerUser(cleanEmail, password, name.trim() || undefined);
      localStorage.removeItem('gatewayllm_master_key');
      localStorage.setItem('gatewayllm_token', res.token);
      localStorage.setItem('gatewayllm_user', JSON.stringify(res.user));
      localStorage.setItem('gatewayllm_onboarding_pending', 'true');
      router.push('/');
      router.refresh();
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Registration failed');
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-zinc-950 px-4">
      <div className="w-full max-w-md gatewayllm-card p-8">
        <h1 className="text-center text-xl font-semibold text-white">
          Gateway<span className="text-emerald-400">-LLM</span>
        </h1>
        <p className="mt-2 text-center text-sm text-zinc-400">Create your free account</p>
        <p className="mt-1 text-center text-xs text-zinc-500">
          You&apos;ll get your own private account to try the gateway with your own provider keys.
        </p>

        <form onSubmit={onSubmit} className="mt-6 space-y-4">
          <div>
            <label htmlFor="name" className="mb-1.5 block text-xs font-medium uppercase tracking-wide text-zinc-500">
              Name (optional)
            </label>
            <input
              id="name"
              type="text"
              autoComplete="name"
              className="gatewayllm-input text-sm"
              placeholder="Jane Doe"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div>
            <label htmlFor="email" className="mb-1.5 block text-xs font-medium uppercase tracking-wide text-zinc-500">
              Email
            </label>
            <input
              id="email"
              type="email"
              autoComplete="email"
              required
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
              autoComplete="new-password"
              required
              minLength={8}
              className="gatewayllm-input text-sm"
              placeholder="At least 8 characters"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <div>
            <label htmlFor="confirm" className="mb-1.5 block text-xs font-medium uppercase tracking-wide text-zinc-500">
              Confirm password
            </label>
            <input
              id="confirm"
              type="password"
              autoComplete="new-password"
              required
              className="gatewayllm-input text-sm"
              placeholder="Repeat password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          </div>
          {err ? <p className="text-sm text-red-400">{err}</p> : null}
          <button type="submit" className="gatewayllm-btn w-full" disabled={loading}>
            {loading ? 'Creating account...' : 'Create account'}
          </button>
        </form>

        <p className="mt-6 text-center text-sm text-zinc-400">
          Already have an account?{' '}
          <Link href="/login" className="text-emerald-400 hover:text-emerald-300">
            Sign in
          </Link>
        </p>

        <p className="mt-4 text-center text-xs text-zinc-500">
          API:{' '}
          <code className="text-zinc-400">
            {process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080'}
          </code>
        </p>
      </div>
    </div>
  );
}
