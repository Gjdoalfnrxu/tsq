export function useState(initial: number): [number, (v: number) => void] {
    let value = initial;
    const setter = (v: number) => { value = v; };
    return [value, setter];
}

export function useConfig(): { host: string; port: number; debug: boolean } {
    return { host: "localhost", port: 3000, debug: true };
}
