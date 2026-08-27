jest.mock('./timeSource');

const { now } = require('./timeSource');

test('now is stable in tests', () => {
  now.mockReturnValue(1700000000);
  expect(now()).toBe(1700000000);
});
