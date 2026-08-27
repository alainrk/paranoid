import { applyCoupon } from '../cart';

it('applies TEN', () => {
  expect(applyCoupon(100, 'TEN')).toBe(90);
});

it('ignores unknown coupons', () => {
  expect(applyCoupon(100, 'NOPE')).toBe(100);
});
