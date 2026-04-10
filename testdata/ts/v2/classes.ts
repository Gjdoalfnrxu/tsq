// Test fixture: classes, interfaces, inheritance, methods, new expressions

interface Serializable {
  serialize(): string;
}

interface Loggable {
  log(msg: string): void;
}

interface Printable extends Serializable {
  print(): void;
}

class Animal {
  name: string;
  constructor(name: string) {
    this.name = name;
  }
  speak(): string {
    return this.name + " speaks";
  }
}

class Dog extends Animal implements Serializable, Loggable {
  breed: string;
  constructor(name: string, breed: string) {
    super(name);
    this.breed = breed;
  }
  speak(): string {
    return this.name + " barks";
  }
  serialize(): string {
    return JSON.stringify({ name: this.name, breed: this.breed });
  }
  log(msg: string): void {
    console.log(msg);
  }
}

class Puppy extends Dog {
  isPlayful: boolean = true;
}

// new expressions
const dog = new Dog("Rex", "Labrador");
const puppy = new Puppy("Buddy", "Poodle");

// method calls
dog.speak();
dog.serialize();
console.log(dog.name);

// type alias
type DogFactory = (name: string, breed: string) => Dog;

// return statements
function createDog(name: string, breed: string): Dog {
  return new Dog(name, breed);
}

// arrow with return
const getName = (d: Dog): string => {
  return d.name;
};
