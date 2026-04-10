import { transformData } from "./transform";

// Cross-file sink: passes tainted data to eval
function execute() {
  const value = transformData();
  eval(value);
}
