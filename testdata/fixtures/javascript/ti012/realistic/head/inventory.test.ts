import { describe, expect, it, vi } from 'vitest';
import { reserve } from './inventory';

vi.mock('./warehouse');

describe('reserve', () => {
  it('reserves stock that exists', () => {
    expect(reserve('sku-1', 2).ok).toBe(true);
  });

  it('rejects short stock', () => {
    expect(reserve('sku-1', 999).ok).toBe(false);
  });
});
