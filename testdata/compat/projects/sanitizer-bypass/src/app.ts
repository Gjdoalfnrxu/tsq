import express from 'express';
import escapeHtml from 'escape-html';
import { escape as sqlEscape } from 'sqlstring';

const app = express();
const db = require('./db');

// Route 1: No sanitizer, tainted data to res.send (XSS) — should alert
const xssNoSanitizer = function(req, res) {
    const q = req.query;
    res.send('<html>' + q + '</html>');
};
app.get('/xss-raw', xssNoSanitizer);

// Route 2: escape-html sanitizer before res.send (XSS) — should NOT alert
const xssCorrectSanitizer = function(req, res) {
    const q = req.query;
    const safe = escapeHtml(q);
    res.send('<html>' + safe + '</html>');
};
app.get('/xss-escaped', xssCorrectSanitizer);

// Route 3: sqlstring.escape before res.send (XSS) — should alert (wrong-kind sanitizer)
const xssWrongSanitizer = function(req, res) {
    const q = req.query;
    const safe = sqlEscape(q);
    res.send('<html>' + safe + '</html>');
};
app.get('/xss-sql-escaped', xssWrongSanitizer);

// Route 4: No sanitizer, tainted data to db.query (SQLi) — should alert
const sqlNoSanitizer = function(req, res) {
    const q = req.query;
    db.query('SELECT * FROM users WHERE id = ' + q);
    res.send('ok');
};
app.get('/sql-raw', sqlNoSanitizer);

// Route 5: sqlstring.escape before db.query (SQL) — should NOT alert
const sqlCorrectSanitizer = function(req, res) {
    const q = req.query;
    const safe = sqlEscape(q);
    db.query('SELECT * FROM users WHERE id = ' + safe);
    res.send('ok');
};
app.get('/sql-escaped', sqlCorrectSanitizer);

// Route 6: escape-html before db.query (SQL) — should alert (wrong-kind sanitizer)
const sqlWrongSanitizer = function(req, res) {
    const q = req.query;
    const safe = escapeHtml(q);
    db.query('SELECT * FROM users WHERE id = ' + safe);
    res.send('ok');
};
app.get('/sql-wrong', sqlWrongSanitizer);
