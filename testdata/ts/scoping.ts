// scoping.ts — nested blocks, shadowing, temporal dead zone

var x = 1;
const tdzConst = 42;
let tdzLet = 7;

function outer() {
  var x = 2; // shadows file-level x (var, function-scoped)
  let y = 10;

  {
    let y = 20; // shadows outer y (let, block-scoped)
    const z = 30;
    // x here resolves to function-level x (var)
    console.log(x, y, z);
  }

  // y here resolves to outer y (block y is gone)
  // z is not in scope here
  console.log(x, y);
}

function tdz() {
  // Temporal dead zone: referencing before declaration
  // let a = b; // would be TDZ violation — b not yet declared
  let b = 5;
  return b;
}

function shadowing() {
  let val = "outer";
  {
    let val = "inner"; // shadows outer val
    console.log(val); // "inner"
  }
  console.log(val); // "outer"
}

// Destructuring
function destructure() {
  const { a, b: renamed } = { a: 1, b: 2 };
  const [first, second] = [10, 20];
  return a + renamed + first + second;
}
