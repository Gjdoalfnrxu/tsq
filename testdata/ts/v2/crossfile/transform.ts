import { getConfig } from "./source";

// Cross-file transform: passes tainted data through unchanged
export function transformData(): string {
  const data = getConfig();
  return data;
}
