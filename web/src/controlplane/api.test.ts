import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from './api';

afterEach(() => vi.unstubAllGlobals());

describe('built-in control-plane client', () => {
  it('uses only the same-origin API and separate bearer admin credential', async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) });
    vi.stubGlobal('fetch', fetch);
    const controller = new AbortController();
    await api('fixture-admin', '/keys', 'POST', { name: 'scoped' }, controller.signal);
    expect(fetch).toHaveBeenCalledWith('/api/control-plane/keys', {
      method: 'POST', signal: controller.signal,
      headers: { Authorization: 'Bearer fixture-admin', 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'scoped' }),
    });
  });

  it('surfaces backend enforcement errors instead of pretending a save succeeded', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 400, json: async () => ({ error: 'invalid provider account binding' }) }));
    await expect(api('fixture-admin', '/keys', 'POST', {})).rejects.toThrow('invalid provider account binding');
  });

  it('never sends the admin credential in the URL', async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ accounts: [] }) });
    vi.stubGlobal('fetch', fetch);
    await api('fixture-secret', '/state');
    expect(fetch.mock.calls[0][0]).not.toContain('fixture-secret');
    expect(fetch.mock.calls[0][1]).not.toHaveProperty('body');
  });
});
