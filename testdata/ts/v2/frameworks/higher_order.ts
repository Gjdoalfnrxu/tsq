// Array.map — array elements flow to callback parameter
const items = [1, 2, 3];
const doubled = items.map((item) => item * 2);

// Array.forEach — array elements flow to callback parameter
items.forEach((item) => {
  console.log(item);
});

// Array.filter — array elements flow to callback parameter
const evens = items.filter((item) => item % 2 === 0);

// Array.reduce — array elements flow to callback second parameter
const sum = items.reduce((acc, item) => acc + item, 0);

// Promise.then — resolved value flows to callback parameter
const promise = fetch("/api/data");
promise.then((response) => {
  return response.json();
});

// Chained promises
fetch("/api/data")
  .then((response) => response.json())
  .then((data) => {
    console.log(data);
  });
