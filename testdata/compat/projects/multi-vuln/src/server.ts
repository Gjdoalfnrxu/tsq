const express = require('express');
const app = express();
const db = require('./db');
const { exec } = require('child_process');
const fs = require('fs');

// Route 1: SQL injection — req.query flows to db.query string concatenation
const sqlHandler = function(req, res) {
    const q = req.query;
    db.query('SELECT ' + q);
    res.send('ok');
};
app.get('/sql', sqlHandler);

// Route 2: XSS — req.query flows to res.send HTML response
const xssHandler = function(req, res) {
    const q = req.query;
    res.send('<html>' + q + '</html>');
};
app.get('/xss', xssHandler);

// Route 3: Command injection — req.query flows to exec shell command
const cmdHandler = function(req, res) {
    const q = req.query;
    exec('ls ' + q);
    res.send('ok');
};
app.get('/cmd', cmdHandler);

// Route 4: Path traversal — req.query flows to fs.readFile path
const pathHandler = function(req, res) {
    const q = req.query;
    fs.readFile('/etc/' + q);
    res.send('ok');
};
app.get('/path', pathHandler);
