// Nested function isolation — inner/outer should not share flow

function outer() {
    let outerVar = 1;

    function inner() {
        let innerVar = 2;
        return innerVar;
    }

    const result = inner();
    return result;
}

const arrowOuter = () => {
    let x = 10;

    const arrowInner = () => {
        let y = 20;
        return y;
    };

    return arrowInner();
};

function withDestructuring() {
    const obj = { a: 1, b: 2 };
    const { a, b } = obj;
    return a;
}
