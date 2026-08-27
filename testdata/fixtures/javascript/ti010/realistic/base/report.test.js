const { render } = require('./report');

test('header', () => {
  const doc = render('q1');
  expect(doc.title).toBe('Q1');
  expect(doc.author).toBe('finance');
  expect(doc.pages).toBeGreaterThan(0);
  expect(doc.toc).not.toBe(null);
});

test('body', () => {
  const doc = render('q1');
  expect(doc.body).toContain('revenue');
  expect(doc.body).toContain('costs');
  expect(doc.body).toContain('outlook');
});

test('footer', () => {
  const doc = render('q1');
  expect(doc.footer.year).toBe(2026);
  expect(doc.footer.pageNumbers).toBe(true);
  expect(doc.footer.contact).toBe('finance@example.com');
});
