import Logger, { helper, VERSION } from "./lib";
import * as lib from "./lib";

const val = helper(42);
const ver = VERSION;
const logger = new Logger();

export function transform(x: number): number {
    return helper(x) * 2;
}
