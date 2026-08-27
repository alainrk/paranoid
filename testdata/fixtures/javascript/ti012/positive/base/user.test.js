const { fetchUser } = require('./user');

test('fetches a user', () => {
  expect(fetchUser(1).name).toBe('ana');
});
