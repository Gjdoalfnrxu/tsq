interface I { f(x: string): string; }
class C implements I { f(x: string): string { return x; } }
new C();
function callIt(o: I, v: string) { return o.f(v); }
const c: I = new C();
callIt(c, "hi");
