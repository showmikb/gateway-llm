'use client';

import { usePathname, useRouter } from 'next/navigation';
import { type ReactNode, useEffect, useState } from 'react';
import { Sidebar } from '@/components/Sidebar';
import { AuthProvider } from '@/contexts/AuthContext';

export function ClientShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const [ready, setReady] = useState(false);

  const isPublicRoute = pathname === '/login' || pathname === '/signup';

  useEffect(() => {
    if (isPublicRoute) {
      setReady(true);
      return;
    }
    const hasAuth =
      localStorage.getItem('gatewayllm_master_key') || localStorage.getItem('gatewayllm_token');
    if (!hasAuth) {
      router.replace('/login');
      return;
    }
    setReady(true);
  }, [pathname, router, isPublicRoute]);

  if (isPublicRoute) {
    return <>{children}</>;
  }

  if (!ready) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-zinc-950 text-zinc-400">
        Loading...
      </div>
    );
  }

  return (
    <AuthProvider>
      <div className="flex h-screen overflow-hidden">
        <Sidebar />
        <main className="h-screen flex-1 overflow-y-auto bg-zinc-950">
          <div className="mx-auto max-w-7xl px-6 py-8">{children}</div>
        </main>
      </div>
    </AuthProvider>
  );
}
