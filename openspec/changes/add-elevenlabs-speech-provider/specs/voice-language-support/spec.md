# Spec Delta

## ADDED Requirements

### Requirement: Voice assignments are isolated by speech provider
The system SHALL maintain separate voice assignments for each speech provider while retaining the shared supported-language list and default language. A provider's voice identifier SHALL NOT be used with another provider.

#### Scenario: Existing assignments survive upgrade
- **WHEN** an installation with saved voice languages is upgraded
- **THEN** its existing voice assignments remain available for the existing provider

#### Scenario: Switching between providers
- **WHEN** an administrator configures distinct voices for both providers and switches between them
- **THEN** new sessions use the active provider's assignments and switching back restores the previously configured voices

#### Scenario: Language has no voice for the active provider
- **WHEN** a non-default supported language has no voice assigned for the active provider
- **THEN** the existing default-language fallback and once-per-session notice apply without using a voice from another provider

#### Scenario: Default voice is required for the selected provider
- **WHEN** an administrator attempts to save settings that leave the active provider's default language without a voice
- **THEN** the system rejects the save and keeps the previous settings
