export function helper(x: number): number {
    return x + 1;
}

export const VERSION = "1.0.0";

export default class Logger {
    log(msg: string) {
        console.log(msg);
    }
}
