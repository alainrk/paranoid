const router = require('express').Router();

router.use(authMiddleware);

router.get('/admin', adminHandler);

module.exports = router;
