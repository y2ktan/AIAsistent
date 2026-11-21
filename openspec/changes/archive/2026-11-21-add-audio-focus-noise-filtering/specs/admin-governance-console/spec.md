# Spec Delta

## ADDED Requirements

### Requirement: Audio focus noise-filtering configuration
The system SHALL enable Audio Focus by default and SHALL allow administrators on `/admin` to enable or disable Audio Focus speaker voice isolation and environmental noise filtering, recording configuration changes in the administrative audit log.

#### Scenario: Audio Focus is enabled by default before manual customization
- **WHEN** an installation initializes or an administrator opens the AI Agent settings on `/admin` before changing Audio Focus
- **THEN** Audio Focus is reported and rendered as enabled (`true`) by default

#### Scenario: Administrator disables or re-enables Audio Focus on /admin
- **WHEN** an administrator toggles Audio Focus off or back on and saves AI Agent settings on `/admin`
- **THEN** the system persists the updated Audio Focus state, applies it to subsequent voice sessions, and records an administrative audit event

#### Scenario: Authenticated caller retrieves active Audio Focus session setting safely
- **WHEN** an authenticated non-admin or admin user starts a voice session
- **THEN** the client receives the active `audioFocusEnabled` boolean without exposing provider URLs, credentials, or admin-only settings
