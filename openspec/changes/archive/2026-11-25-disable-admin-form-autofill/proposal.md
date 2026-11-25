# Proposal

## Why

Browser password managers currently autofill the logged-in administrator's saved login email (e.g., `chunmuntan@yahoo.com`) and password into the **Add User** and **Register MCP** forms on `/admin` because those forms lack explicit autocomplete suppression attributes on their email/text and password/token inputs. This creates confusion and risks accidentally submitting or exposing the administrator's own credentials when creating a new user or registering an MCP server.

## What Changes

- Suppress browser credential autofill on the `/admin` **Add User** form (`email` and `password` inputs) so fields always render empty by default.
- Suppress browser credential autofill on the `/admin` **Register MCP** form (`Server Name`, `URL`, and `Bearer token` password input) so fields always render empty by default.
- Add unit/component tests verifying the `/admin` user creation and MCP registration inputs include autocomplete suppression attributes and initialize with empty values.

## Capabilities

### New Capabilities

### Modified Capabilities
- `admin-governance-console`: Require `/admin` user creation and MCP server registration forms to suppress browser credential autofill so saved admin login emails and passwords do not pre-populate those fields.

## Impact

- Affected code: `web/src/app/admin/page.tsx` (Add User form and Register MCP form inputs).
- Affected tests: `web/src/__tests__/` (new or updated test covering admin form autofill attributes and empty initial values).
- No backend API, database schema, or dependency changes required.
