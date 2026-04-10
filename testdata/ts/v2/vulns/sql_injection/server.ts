import express from "express";

const app = express();

// VULNERABLE: unsanitized user input passed to db.query
app.get("/user", (req, res) => {
  const userId = req.query.id;
  const sql = "SELECT * FROM users WHERE id = " + userId;
  db.query(sql);
  res.send("done");
});
