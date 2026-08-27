function save(record) {
  return db.insert(record);
}
module.exports = { save };
