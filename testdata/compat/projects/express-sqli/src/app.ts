const express = require('express');
const app = express();
const db = require('./db');

// Route 1: SQL injection via req.query string concatenation
const usersHandler = function(req, res) {
    const id = req.query;
    db.query('SELECT * FROM users WHERE id = ' + id);
    res.send('ok');
};
app.get('/users', usersHandler);

// Route 2: SQL injection via req.body string concatenation
const loginHandler = function(req, res) {
    const username = req.body;
    db.query("SELECT * FROM accounts WHERE name = '" + username + "'");
    res.send('ok');
};
app.post('/login', loginHandler);

// Route 3: Safe parameterized query
const safeHandler = function(req, res) {
    const id = req.query;
    db.query('SELECT * FROM users WHERE id = ?', [id]);
    res.send('ok');
};
app.get('/safe', safeHandler);
