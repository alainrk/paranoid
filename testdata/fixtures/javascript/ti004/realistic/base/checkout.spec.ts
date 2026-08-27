import { checkout } from './checkout';

describe('checkout', () => {
  it('charges the card', () => {
    expect(checkout(cart()).charged).toBe(true);
  });

  it('empties the cart', () => {
    expect(checkout(cart()).cart.items).toHaveLength(0);
  });
});
