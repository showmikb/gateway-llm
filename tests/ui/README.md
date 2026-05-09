# Gateway-LLM UI test toolkit

Playwright-based end-to-end suite for the Next.js admin dashboard. Runs
against production by default, points anywhere via env vars. Every
`page.tsx` under `gateway-llm/ui/src/app/` is smoke-tested automatically
the next time you run the suite - no manual wiring when new pages land.

## Quickstart

```bash
cd gateway-llm/tests/ui
./run.sh
```

First run installs npm deps and the Chromium browser bundle. Subsequent
runs are incremental.

## What it covers

| Spec                        | What it guards                                         |
| --------------------------- | ------------------------------------------------------ |
| `00-smoke.spec.ts`          | **Auto-discovered**: every page renders, no 5xx, no console errors |
| `01-public.spec.ts`         | Login + signup page affordances, client-side validation |
| `02-auth-redirects.spec.ts` | Every authenticated page bounces anonymous visitors to `/login` |
| `03-signup-login.spec.ts`   | Real `/signup` POSTs through, returning user can log in |
| `04-user-journey.spec.ts`   | org_admin's Dashboard -> Credentials -> Models -> Keys path |
| `05-credentials.spec.ts`    | Add + delete provider key via the UI                   |
| `06-models.spec.ts`         | Add + delete a model route                             |
| `07-keys.spec.ts`           | Create API key, reveal once, row shows up masked       |
| `08-rbac.spec.ts`           | member/viewer sidebars, read-only banner               |
| `09-master-admin.spec.ts`   | Master key sees every org and every page               |
| `10-sidebar-nav.spec.ts`    | Every sidebar link an org_admin sees lands correctly   |

## How new pages auto-join

`helpers/routes.ts` walks `gateway-llm/ui/src/app/` at test-collection
time and emits a `UiPage` record per `page.tsx`. Both `00-smoke` and
`02-auth-redirects` iterate that list, so adding a new page - e.g.
`ui/src/app/billing/page.tsx` - automatically produces:

- A smoke test asserting it renders without errors
- A redirect test asserting unauthenticated visitors get bounced

If the new page uses new UI affordances you want covered explicitly,
drop a dedicated spec alongside the others. Otherwise, the auto-coverage
is usually enough for day-to-day regression protection.

## Environment variables

| Variable          | Default                         | Purpose                                                  |
| ----------------- | ------------------------------- | -------------------------------------------------------- |
| `UI_URL`          | `https://app.gateway-llm.com`   | Frontend base URL                                        |
| `API_URL`         | `https://api.gateway-llm.com`   | Backend base URL (used for setup/teardown)               |
| `MASTER_KEY`      | `showmikbose@1995`              | Admin master key for provisioning + master-admin spec    |
| `TEST_PASSWORD`   | `Hunter2-gatewayllm!`           | Password for provisioned accounts                        |
| `RUN_REAL_SIGNUP` | `true`                          | Set `false` to skip the real `/signup` test (rate limit) |
| `KEEP_TEST_DATA`  | `0`                             | Set `1` to skip teardown (useful when debugging)         |
| `CI`              | unset                           | Enables retries and `--forbid-only`                      |

## Workflow when something breaks

1. `./run.sh` fails.
2. Open the HTML report: `npx playwright show-report`. Every failure has
   a screenshot, a trace (timeline + DOM snapshots), and a video.
3. Fix the issue in `gateway-llm/ui` or `gateway-llm/backend`.
4. Redeploy (`deploy/*.sh` in the repo root).
5. Re-run just the failing spec: `./run.sh 05-credentials`.
6. Once green, run the full suite again: `./run.sh`.

## House rules

- Global setup provisions one ui-test org + three users (org_admin,
  member, viewer) and one secondary org. Global teardown deletes them.
- The suite uses `workers: 1` because all tests share that single org.
  If we ever need parallelism, split accounts into per-worker buckets.
- Anything requiring a real LLM call (e.g. `credential.test`) is kept
  out of the UI toolkit; the backend API test scripts cover that.
- `.auth/accounts.json` is a per-run scratch file; it's gitignored and
  deleted by teardown.
