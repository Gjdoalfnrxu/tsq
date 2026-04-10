// imports.ts — various import forms

import defaultExport from "./module";
import { named1, named2 } from "./module";
import { original as alias } from "./module";
import * as namespace from "./module";
import defaultExport2, { named3 } from "./module2";

// Use the imports
const a = defaultExport();
const b = named1 + named2;
const c = alias;
const d = namespace.something;
const e = defaultExport2 + named3;

export function exportedFn() {
  return 42;
}

export const exportedConst = "hello";

export default function defaultFn() {
  return true;
}
