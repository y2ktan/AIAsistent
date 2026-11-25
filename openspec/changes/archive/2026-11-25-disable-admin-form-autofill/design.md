# Design

## Context

In `web/src/app/admin/page.tsx`, both the **Add User** form (Users tab) and the **Register MCP** form (MCP tab) initialize their React state to empty strings (`""`), yet browsers with saved login credentials for the site automatically fill the administrator's saved email and password into those inputs because:
1. Both forms contain `<input type="password">` without `autoComplete="new-password"`.
2. Their preceding `<input type="email">` / `<input type="text">` / `<input type="url">` fields lack `autoComplete="off"` and distinct non-login `name` attributes.

See `proposal.md` for motivation.

## Goals / Non-Goals

**Goals:**
- Prevent browser password managers from pre-filling saved login emails or passwords into the Add User and Register MCP forms on `/admin`.
- Keep all form inputs starting empty with unchanged submission payloads and validation behavior.

**Non-Goals:**
- Disabling browser autofill on `/login` (where saved credentials are expected).
- Changing backend API endpoints, validation, or MCP/user persistence.

## Decisions

- **Use `autoComplete="new-password"` on password/token inputs and `autoComplete="off"` on form/text/email/url inputs**:
  - Modern Chromium, Safari, and Firefox password managers intentionally ignore `autoComplete="off"` on `<input type="password">` and its preceding username-like field, but respect `autoComplete="new-password"` (or `one-time-code`) on password fields alongside explicit `autoComplete="off"` on the form and non-password inputs.
  - **Alternatives considered**:
    - Using `readOnly` until focus (`onFocus` removal): adds UX friction, breaks screen-reader expectations, and complicates React controlled input behavior.
    - Changing `type="password"` to `type="text"` with CSS masking (`-webkit-text-security`): non-standard across browsers and weakens native password masking.
- **Assign explicit non-login `name` attributes** (e.g., `new-user-email`, `new-user-password`, `mcp-server-name`, `mcp-server-url`, `mcp-bearer-token`):
  - Prevents heuristics from matching generic `email`/`username`/`password` field names.

## Risks / Trade-offs

- [Browser heuristic variance] → Different browsers/password-manager extensions (e.g., 1Password, Bitwarden) may still offer an explicit dropdown on focus, which is acceptable; `autoComplete="new-password"` + `autoComplete="off"` + explicit `name` prevents automatic silent pre-filling on render.
