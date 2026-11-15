# Tasks

## 1. Suppress Autofill on Admin Forms

- [x] 1.1 Update the **Add User** form in `web/src/app/admin/page.tsx` with `autoComplete="off"` on the `<form>` and email input (`name="new-user-email"`), and `autoComplete="new-password"` on the password input (`name="new-user-password"`), and verify `npm --prefix web run lint` passes
- [x] 1.2 Update the **Register MCP** form in `web/src/app/admin/page.tsx` with `autoComplete="off"` on the `<form>`, server name input (`name="mcp-server-name"`), and URL input (`name="mcp-server-url"`), and `autoComplete="new-password"` on the bearer token input (`name="mcp-bearer-token"`), and verify `npm --prefix web run lint` passes

## 2. Automated Verification

- [x] 2.1 Add a Vitest unit test in `web/src/__tests__/admin-form-autofill.test.ts` verifying that the Add User and Register MCP forms in `web/src/app/admin/page.tsx` initialize with empty values and carry the expected `autoComplete` and `name` attributes, and verify `npm --prefix web test` passes
- [x] 2.2 Run `npm --prefix web run build` and `npm --prefix web test` to confirm the Next.js production build and full test suite pass with no regressions
