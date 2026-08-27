const { total } = require('./cart');

describe('total', () => {
  it('sums item prices', () => {
    expect(total([{ price: 5 }, { price: 7 }])).toBe(12);
  });

  it('handles the empty cart', () => {
    expect(total([])).toBe(0);
  });
});
