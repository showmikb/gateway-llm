import fs from 'fs';
import path from 'path';

/**
 * Walk ui/src/app/ at test-collection time and return every routable
 * Next.js page. Any new `page.tsx` added by a dev is picked up
 * automatically on the next run, so the smoke suite grows with the app
 * without manual bookkeeping.
 */
const APP_DIR = path.resolve(__dirname, '..', '..', '..', 'ui', 'src', 'app');

export interface UiPage {
  /** URL path served by Next.js (e.g. "/", "/keys", "/signup"). */
  route: string;
  /** Absolute filesystem path to the page.tsx file. */
  file: string;
  /** True when the page is reachable without a logged-in session. */
  isPublic: boolean;
}

const PUBLIC_ROUTES = new Set(['/login', '/signup']);

function walk(dir: string, acc: UiPage[] = []): UiPage[] {
  if (!fs.existsSync(dir)) return acc;
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      // Skip Next.js special folders that don't produce routes we care about.
      if (entry.name.startsWith('_') || entry.name.startsWith('(')) continue;
      walk(full, acc);
    } else if (entry.isFile() && entry.name === 'page.tsx') {
      const rel = path.relative(APP_DIR, dir);
      const route = rel === '' ? '/' : '/' + rel.split(path.sep).join('/');
      acc.push({
        route,
        file: full,
        isPublic: PUBLIC_ROUTES.has(route),
      });
    }
  }
  return acc;
}

let cached: UiPage[] | null = null;

export function discoverPages(): UiPage[] {
  if (cached) return cached;
  cached = walk(APP_DIR).sort((a, b) => a.route.localeCompare(b.route));
  return cached;
}

export function authenticatedRoutes(): UiPage[] {
  return discoverPages().filter((p) => !p.isPublic);
}

export function publicRoutes(): UiPage[] {
  return discoverPages().filter((p) => p.isPublic);
}
