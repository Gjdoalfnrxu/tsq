import { add, multiply } from "./utils";

const result = add(1, 2);
const product = multiply(3, 4, 5);
console.log(result, product);

function processData(a: number, b: number, c: number, d: number) {
    return add(a, b) + multiply(c, d, a, b);
}
