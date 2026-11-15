# Spec Delta

## ADDED Requirements

### Requirement: Admin creation and MCP registration forms suppress browser credential autofill
The system SHALL configure the `/admin` user creation form and MCP server registration form to suppress browser credential autofill so that saved administrator login emails and passwords are not pre-populated into those inputs by default.

#### Scenario: Add User form renders empty without credential autofill
- **WHEN** an administrator with saved browser credentials opens the Users tab on `/admin`
- **THEN** the Add User email and password inputs initialize empty and declare autocomplete suppression attributes so the browser does not pre-fill the administrator's saved email or password

#### Scenario: Register MCP form renders empty without credential autofill
- **WHEN** an administrator with saved browser credentials opens the MCP tab on `/admin`
- **THEN** the Register MCP server name, URL, and bearer token inputs initialize empty and declare autocomplete suppression attributes so the browser does not pre-fill the administrator's saved email or password
