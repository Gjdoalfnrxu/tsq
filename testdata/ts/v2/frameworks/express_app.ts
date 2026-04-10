import express from "express";

const app = express();

// GET handler — req.query is a taint source, res.send is a taint sink
app.get("/search", (req, res) => {
  const query = req.query.q;  // source: http_input
  res.send(query);            // sink: xss
});

// POST handler — req.body is a taint source
app.post("/submit", (req, res) => {
  const data = req.body;      // source: http_input
  res.send(data);             // sink: xss
});

// req.params source
app.get("/user/:id", (req, res) => {
  const id = req.params.id;   // source: http_input
  res.send(`User ${id}`);     // sink: xss
});

app.listen(3000);
