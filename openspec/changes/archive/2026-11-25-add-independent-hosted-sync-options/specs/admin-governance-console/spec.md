# Spec Delta

## ADDED Requirements

### Requirement: Independent hosted synchronization controls
The administration console SHALL provide separate controls for synchronizing local RAG documents and enabled Agent Skills to a hosted ElevenLabs agent, SHALL default both controls to enabled for existing and new configurations, and SHALL show the selected ownership mode and synchronization state independently.

#### Scenario: Administrator disables only RAG synchronization
- **WHEN** an administrator disables application RAG synchronization while leaving Agent Skill synchronization enabled
- **THEN** hosted activation no longer uploads or attaches application documents, enabled Agent Skills continue to synchronize, and the console clearly identifies RAG as application-sync disabled

#### Scenario: Administrator disables only Agent Skill synchronization
- **WHEN** an administrator disables Agent Skill synchronization while leaving application RAG synchronization enabled
- **THEN** hosted prompt synchronization excludes enabled application skills, local documents continue to synchronize, and the console clearly identifies Agent Skills as application-sync disabled

#### Scenario: Administrator disables both synchronization domains
- **WHEN** an administrator disables application RAG and Agent Skill synchronization
- **THEN** the hosted agent can be activated and used with only the independently configured persona and system instructions, without application documents or skills blocking connection

#### Scenario: An existing configuration is upgraded
- **WHEN** an installation with an existing hosted-agent configuration receives this capability
- **THEN** both synchronization controls are treated as enabled until an administrator explicitly changes them

#### Scenario: Administrator changes a disabled domain
- **WHEN** an administrator enables or disables either synchronization control on an active hosted configuration
- **THEN** the system saves the choice transactionally with hosted compatibility verification and reports any required provider cleanup or synchronization failure without representing the domain as successfully changed
