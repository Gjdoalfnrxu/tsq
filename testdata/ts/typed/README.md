# Typed TypeScript Fixtures

Test fixtures for type-checker integration. Each file exercises specific TypeScript type-system features.

| File | Features |
|---|---|
| `generics.ts` | Generic function, generic class, bounded generic (`T extends Interface`) |
| `conditional.ts` | Conditional types (`T extends U ? X : Y`), `infer` keyword, distributive conditionals |
| `mapped.ts` | Mapped types (`{ [K in keyof T]: ... }`), key remapping, readonly/optional modifiers |
| `union_intersection.ts` | Union types, intersection types, discriminated unions, type narrowing |
| `literal_types.ts` | String literal types, numeric literal types, template literal types, `as const` |
