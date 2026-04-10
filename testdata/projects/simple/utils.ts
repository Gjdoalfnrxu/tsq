export function add(x: number, y: number): number {
    return x + y;
}

export function multiply(...args: number[]): number {
    return args.reduce((acc, val) => acc * val, 1);
}

export function greet(name: string): string {
    return `Hello, ${name}!`;
}
