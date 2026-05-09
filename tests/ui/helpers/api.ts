import { env } from './env';

/**
 * Minimal HTTP client for the Gateway-LLM management API. Used by setup
 * and teardown to provision and clean up test data without going through
 * the UI (so we don't burn the public /register rate limit).
 */
export class ApiClient {
  constructor(private token: string = env.MASTER_KEY, private base: string = env.API_URL) {}

  async request<T = any>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await fetch(`${this.base}${path}`, {
      method,
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${this.token}`,
      },
      body: body ? JSON.stringify(body) : undefined,
    });
    const text = await res.text();
    if (!res.ok) {
      throw new Error(`${method} ${path} -> ${res.status}: ${text.slice(0, 400)}`);
    }
    return text ? (JSON.parse(text) as T) : (undefined as any as T);
  }

  // Registration is public, no auth needed.
  static async register(email: string, password: string, name?: string) {
    const res = await fetch(`${env.API_URL}/v1/management/users/register`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password, name }),
    });
    const text = await res.text();
    if (!res.ok) throw new Error(`register -> ${res.status}: ${text}`);
    return JSON.parse(text);
  }

  static async login(email: string, password: string) {
    const res = await fetch(`${env.API_URL}/v1/management/users/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    });
    const text = await res.text();
    if (!res.ok) throw new Error(`login -> ${res.status}: ${text}`);
    return JSON.parse(text);
  }
}
