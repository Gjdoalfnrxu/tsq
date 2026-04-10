import { exec } from "child_process";
import * as fs from "fs";

// Command injection sink — exec with user input
function runCommand(userInput: string) {
  exec(userInput);  // sink: command_injection
}

// File system operations
function readFile(path: string) {
  return fs.readFileSync(path, "utf-8");
}

function writeFile(path: string, content: string) {
  fs.writeFileSync(path, content);
}
