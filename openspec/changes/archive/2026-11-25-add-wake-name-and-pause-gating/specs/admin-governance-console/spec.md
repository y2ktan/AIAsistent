# Spec Delta

## ADDED Requirements

### Requirement: Assistant wake-name and alias configuration
The system SHALL default the assistant name to `Aegis` with no wake aliases and SHALL allow administrators on `/admin` to configure a non-empty primary assistant name and optional wake aliases in English or Chinese, recording updates in the administrative audit log and exposing the active primary name safely to authenticated voice sessions.

#### Scenario: Default assistant name reported before customization
- **WHEN** an installation initializes or an administrator opens the AI Agent settings on `/admin` before customizing the assistant name
- **THEN** the primary assistant name is reported and rendered as `Aegis` with an empty wake alias list

#### Scenario: Administrator saves English and Chinese wake names on /admin
- **WHEN** an administrator sets a primary assistant name (such as `Jarvis` or `小智`) and optional wake aliases (such as `["小智", "Aegis"]`) and saves AI Agent settings on `/admin`
- **THEN** the system persists the trimmed primary name and deduplicated wake aliases, applies them to subsequent voice sessions, and records an administrative audit entry

#### Scenario: Blank primary assistant name rejected
- **WHEN** an administrator submits an empty or whitespace-only primary assistant name on `/admin`
- **THEN** the system rejects the request with HTTP `400 Bad Request` and leaves the stored configuration unchanged

#### Scenario: Authenticated caller retrieves configured assistant name safely
- **WHEN** an authenticated non-admin or admin user fetches `/api/v1/rtc/config` before starting a voice session
- **THEN** the response includes the active `assistantName` alongside `audioFocusEnabled` without exposing provider URLs, credentials, or admin-only settings
