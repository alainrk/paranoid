import { total } from '../cart';

it('sums prices', () => {
  expect(total([{ price: 5 }, { price: 7 }])).toBe(12);
});

it('handles empty carts', () => {
  expect(total([])).toBe(0);
});
