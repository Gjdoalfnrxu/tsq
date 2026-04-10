// Multi-step flow chains

function chainFlow() {
    let a = 1;
    let b = a;
    let c = b;
    let d = c;
    let e = d;
    return e;
}

function branchFlow() {
    let source = "data";
    let left = source;
    let right = source;
    let leftResult = left;
    let rightResult = right;
    return leftResult;
}

function fieldFlow() {
    const obj: any = {};
    obj.field = "value";
    const read = obj.field;
    return read;
}
