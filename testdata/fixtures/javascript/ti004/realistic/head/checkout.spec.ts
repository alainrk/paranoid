import { checkout } from './checkout';

describe.skip('checkout', () => {
  it('charges the card', () => {
    expect(checkout(cart()).charged).toBe(true);
  });

  it('empties the cart', () => {
    expect(checkout(cart()).cart.items).toHaveLength(0);
  });
});
