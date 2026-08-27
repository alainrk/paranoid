export function total(items) {
  return items.reduce((sum, item) => sum + item.price, 0);
}
export function applyCoupon(total, coupon) {
  return coupon === 'TEN' ? total * 0.9 : total;
}
