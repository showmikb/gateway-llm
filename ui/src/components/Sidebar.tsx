'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useEffect, useState, type ReactNode } from 'react';
import { useAuth } from '@/contexts/AuthContext';
import { useSetupState } from '@/lib/setupState';

interface NavItem {
  href: string;
  label: string;
  minRole: string;
  icon: ReactNode;
}

function Icon({ path }: { path: string }) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d={path} />
    </svg>
  );
}

const ICONS: Record<string, ReactNode> = {
  dashboard: <Icon path="M3 13h8V3H3zM13 21h8V11h-8zM3 21h8v-6H3zM13 3v6h8V3z" />,
  credentials: <Icon path="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4" />,
  routes: <Icon path="M4 6h16M4 12h10M4 18h16" />,
  keys: <Icon path="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zM14 8l3 3" />,
  usage: <Icon path="M3 3v18h18M7 14l4-4 4 4 5-5" />,
  observability: <Icon path="M22 12h-4l-3 9L9 3l-3 9H2" />,
  savings: <Icon path="M12 1v22M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6" />,
  routing: <Icon path="M3 6h6l3 6 3-6h6M3 18h6l3-6 3 6h6" />,
  teams: <Icon path="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zm14 10v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" />,
  users: <Icon path="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zm13-2v6m3-3h-6" />,
  pricing: <Icon path="M12 1v22M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6" />,
  settings: <Icon path="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zm7.4-3a7.42 7.42 0 0 0-.1-1.2l2-1.5-2-3.5-2.4.9a7.5 7.5 0 0 0-2-1.2L14.4 3h-4l-.5 2.5a7.5 7.5 0 0 0-2 1.2l-2.4-.9-2 3.5 2 1.5c-.07.4-.1.8-.1 1.2s.03.8.1 1.2l-2 1.5 2 3.5 2.4-.9a7.5 7.5 0 0 0 2 1.2L9.6 21h4l.5-2.5a7.5 7.5 0 0 0 2-1.2l2.4.9 2-3.5-2-1.5c.07-.4.1-.8.1-1.2z" />,
  organizations: <Icon path="M3 21h18M3 7l9-4 9 4M5 21V10m14 11V10M9 21v-6h6v6" />,
};

const allNav: NavItem[] = [
  { href: '/', label: 'Dashboard', minRole: 'viewer', icon: ICONS.dashboard },
  { href: '/credentials', label: 'Provider Keys', minRole: 'org_admin', icon: ICONS.credentials },
  { href: '/models', label: 'Model Routes', minRole: 'viewer', icon: ICONS.routes },
  { href: '/keys', label: 'API Keys', minRole: 'viewer', icon: ICONS.keys },
  { href: '/usage', label: 'Usage', minRole: 'viewer', icon: ICONS.usage },
  { href: '/savings', label: 'Savings', minRole: 'viewer', icon: ICONS.savings },
  { href: '/routing', label: 'Routing', minRole: 'org_admin', icon: ICONS.routing },
  { href: '/observability', label: 'Observability', minRole: 'viewer', icon: ICONS.observability },
  { href: '/teams', label: 'Teams', minRole: 'team_admin', icon: ICONS.teams },
  { href: '/users', label: 'Users', minRole: 'org_admin', icon: ICONS.users },
  { href: '/pricing', label: 'Pricing', minRole: 'org_admin', icon: ICONS.pricing },
  { href: '/settings', label: 'Settings', minRole: 'org_admin', icon: ICONS.settings },
  { href: '/organizations', label: 'Organizations', minRole: 'super_admin', icon: ICONS.organizations },
];

const roleLevels: Record<string, number> = {
  super_admin: 4,
  org_admin: 3,
  admin: 3,
  team_admin: 2,
  member: 1,
  user: 1,
  viewer: 0,
};

function hasMinRole(userRole: string | undefined, minRole: string): boolean {
  if (!userRole) return false;
  return (roleLevels[userRole] ?? 0) >= (roleLevels[minRole] ?? 0);
}

const COLLAPSE_KEY = 'gatewayllm_sidebar_collapsed';

export function Sidebar() {
  const pathname = usePathname();
  const { isMasterKey, user, logout } = useAuth();
  const setup = useSetupState();

  const [collapsed, setCollapsed] = useState(false);
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    if (typeof window !== 'undefined') {
      setCollapsed(window.localStorage.getItem(COLLAPSE_KEY) === '1');
      setHydrated(true);
    }
  }, []);

  function toggle() {
    const next = !collapsed;
    setCollapsed(next);
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(COLLAPSE_KEY, next ? '1' : '0');
    }
  }

  const nav = allNav.filter((item) => {
    if (isMasterKey) return true;
    return hasMinRole(user?.role, item.minRole);
  });

  const pulseHref: Record<string, boolean> = {
    '/credentials': !setup.loading && !setup.hasProviderKeys,
    '/models': !setup.loading && setup.hasProviderKeys && !setup.hasDeployments,
    '/keys': !setup.loading && setup.hasDeployments && !setup.hasAPIKeys,
  };

  const completedSetup =
    (setup.hasProviderKeys ? 1 : 0) +
    (setup.hasDeployments ? 1 : 0) +
    (setup.hasAPIKeys ? 1 : 0);
  const setupIncomplete = !setup.loading && completedSetup < 3;

  const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

  const widthClass = collapsed ? 'w-14' : 'w-56';

  return (
    <aside
      className={`flex ${widthClass} shrink-0 flex-col border-r border-zinc-800 bg-zinc-950 transition-[width] duration-150`}
      aria-label="Primary navigation"
      data-collapsed={collapsed ? 'true' : 'false'}
    >
      <div
        className={`flex h-14 shrink-0 items-center border-b border-zinc-800 ${
          collapsed ? 'justify-center px-2' : 'justify-between px-4'
        }`}
      >
        {!collapsed && (
          <Link href="/" className="text-lg font-semibold tracking-tight text-white">
            Gateway<span className="text-emerald-400">-LLM</span>
          </Link>
        )}
        {hydrated && (
          <button
            type="button"
            onClick={toggle}
            className="rounded-md p-1.5 text-zinc-400 hover:bg-zinc-800/60 hover:text-zinc-200"
            aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            title={collapsed ? 'Expand' : 'Collapse'}
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden
              style={{ transform: collapsed ? 'rotate(180deg)' : 'none' }}
            >
              <path d="M15 18l-6-6 6-6" />
            </svg>
          </button>
        )}
      </div>
      <nav className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto p-3">
        {nav.map((item) => {
          const active =
            pathname === item.href || (item.href !== '/' && pathname.startsWith(item.href));
          const pulse = pulseHref[item.href];
          const showSetupBadge = item.href === '/' && setupIncomplete;
          return (
            <Link
              key={item.href}
              href={item.href}
              title={collapsed ? item.label : undefined}
              className={`group relative flex items-center ${
                collapsed ? 'justify-center' : 'justify-between'
              } gap-2 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                active
                  ? 'bg-emerald-500/10 text-emerald-300'
                  : 'text-zinc-400 hover:bg-zinc-800/60 hover:text-zinc-200'
              }`}
            >
              <span className={`flex items-center gap-3 ${collapsed ? 'justify-center' : ''}`}>
                <span className="shrink-0 text-zinc-500 group-hover:text-zinc-300">{item.icon}</span>
                {!collapsed && <span>{item.label}</span>}
              </span>
              {!collapsed && (
                <span className="flex items-center gap-2">
                  {showSetupBadge && (
                    <span
                      className="rounded-full border border-emerald-500/30 bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-emerald-300"
                      aria-label={`Setup ${completedSetup} of 3 complete`}
                    >
                      {completedSetup}/3
                    </span>
                  )}
                  {pulse && (
                    <span className="relative flex h-2 w-2" aria-label="Setup required">
                      <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                      <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-400" />
                    </span>
                  )}
                </span>
              )}
              {collapsed && pulse && (
                <span className="absolute right-1 top-1 flex h-2 w-2" aria-hidden>
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                  <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-400" />
                </span>
              )}
            </Link>
          );
        })}
      </nav>
      <div className={`shrink-0 border-t border-zinc-800 ${collapsed ? 'p-2' : 'p-3'} space-y-2`}>
        {!collapsed && (
          <div
            className="rounded-lg border border-zinc-800 bg-zinc-900/40 px-3 py-2"
            aria-label="Gateway endpoint"
          >
            <p className="text-[10px] font-medium uppercase tracking-wide text-zinc-500">Endpoint</p>
            <p
              className="mt-0.5 truncate font-mono text-[11px] text-emerald-300"
              title={`${apiUrl}/v1`}
            >
              {apiUrl.replace(/^https?:\/\//, '')}/v1
            </p>
          </div>
        )}
        {!collapsed && user && (
          <div className="px-3 py-1.5 text-xs text-zinc-500 truncate">
            {user.email}
            <span className="ml-1 rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] uppercase">
              {user.role}
            </span>
          </div>
        )}
        {!collapsed && isMasterKey && (
          <div className="px-3 py-1.5 text-xs text-amber-500/80">Master Key</div>
        )}
        <button
          onClick={logout}
          title={collapsed ? 'Logout' : undefined}
          className={`flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-zinc-500 transition-colors hover:bg-zinc-800/60 hover:text-zinc-300 ${
            collapsed ? 'justify-center' : ''
          }`}
          aria-label="Logout"
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden
          >
            <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" />
          </svg>
          {!collapsed && <span>Logout</span>}
        </button>
      </div>
    </aside>
  );
}
