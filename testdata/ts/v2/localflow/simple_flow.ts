// Simple assignment and return flow

function simpleAssign() {
    let x = 10;
    let y = x;
    return y;
}

function multiAssign() {
    let a = 1;
    let b = a;
    let c = b;
    a = c;
    return a;
}

function withReturn(input: number): number {
    const result = input;
    return result;
}
