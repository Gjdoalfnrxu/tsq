// arrow_functions.ts — arrow function forms and closures

const double = (x: number) => x * 2;

const add = (a: number, b: number): number => {
  return a + b;
};

const noArgs = () => 42;

const withBody = (name: string) => {
  const greeting = "Hello, " + name;
  return greeting;
};

// Closures
function makeCounter() {
  let count = 0;
  return {
    increment: () => ++count,
    decrement: () => --count,
    value: () => count,
  };
}

// Higher-order
const numbers = [1, 2, 3, 4, 5];
const doubled = numbers.map((n) => n * 2);
const evens = numbers.filter((n) => n % 2 === 0);

// Async arrows
const fetchData = async (url: string) => {
  const response = await fetch(url);
  return response.json();
};

// Nested arrows
const compose = (f: (x: number) => number, g: (x: number) => number) =>
  (x: number) => f(g(x));
