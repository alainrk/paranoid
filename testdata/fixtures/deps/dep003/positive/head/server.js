const express = require('express');
const widget = require('dusty-widget');

const app = express();
app.use(widget());
