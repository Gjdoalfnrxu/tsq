import express from "express";

const app = express();

// SAFE: parameterized query
app.get("/user", (req, res) => {
  const userId = req.query.id;
  db.query("SELECT * FROM users WHERE id = ?", [userId]);
  res.send("done");
});
