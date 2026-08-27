import { login, logout } from './session';

describe('session', () => {
  it('logs in with valid credentials', () => {
    expect(login('ana', 'pw').ok).toBe(true);
    expect(login('ana', 'pw').user).toBe('ana');
  });

  it('rejects bad passwords', () => {
    expect(login('ana', 'wrong').ok).toBe(false);
  });

  it('logs out', () => {
    expect(logout('ana').ok).toBe(true);
    expect(logout('ana').user).toBe(null);
  });
});
