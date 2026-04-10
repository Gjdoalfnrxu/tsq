// Test fixture: XSS — tainted HTTP input flows to HTML output.
import { Request, Response } from "express";

function renderPage(req: Request, res: Response) {
    const username = req.params.username; // TaintSource: http_input
    const html = "<h1>Hello, " + username + "</h1>";
    res.send(html); // TaintSink: xss
}
