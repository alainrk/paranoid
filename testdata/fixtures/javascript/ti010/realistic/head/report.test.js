const { render } = require('./report');

test('header', () => {
  const doc = render('q1');
  expect(doc.title).toBe('Q1');
  expect(doc.author).toBe('finance');
});

test('body', () => {
  const doc = render('q1');
  expect(doc.body).toContain('revenue');
  expect(doc.body).toContain('costs');
});

test('footer', () => {
  const doc = render('q1');
  expect(doc.footer.year).toBe(2026);
  expect(doc.footer.pageNumbers).toBe(true);
  expect(doc.footer.contact).toBe('finance@example.com');
});
