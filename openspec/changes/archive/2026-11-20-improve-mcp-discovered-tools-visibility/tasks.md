# Tasks

## 1. MCP Discovered-Tools Visibility

- [x] 1.1 Add per-server Show Tools / Hide Tools controls in the admin MCP cards, keeping lists expanded by default and preserving visibility across checkbox updates and discovery refreshes; verify the behavior with the focused admin UI tests.
- [x] 1.2 Extend the admin UI tests to cover hiding one server's tool list without hiding its card or changing its allowlist, independent visibility across servers, and checkbox updates preserving the current visibility; verify with `npx vitest run src/__tests__/admin-form-autofill.test.ts` from `web/`.

## 2. Frontend Verification

- [x] 2.1 Run the full frontend tests and production build with `npm test` and `npm run build` from `web/`, confirming the MCP visibility change introduces no regressions.
