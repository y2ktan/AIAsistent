# Spec Delta

## ADDED Requirements

### Requirement: Optional hosted application augmentation
Hosted-agent connection SHALL remain available when application RAG synchronization, Agent Skill synchronization, or both are disabled, provided the configured private agent, authentication, workflow, and tool references remain valid.

#### Scenario: Hosted agent connects without application RAG or skills
- **WHEN** a private hosted agent is configured with both application RAG and Agent Skill synchronization disabled
- **THEN** the application validates and issues hosted access without requiring local ready documents, local Agent Skills, or an enabled hosted RAG configuration

#### Scenario: Hosted agent connects with only one application domain
- **WHEN** either application RAG or Agent Skill synchronization is enabled while the other is disabled
- **THEN** hosted access uses only the enabled application domain and does not require the disabled domain to be ready or available

#### Scenario: Dashboard-owned hosted knowledge remains available
- **WHEN** application RAG synchronization is disabled and the ElevenLabs agent retains dashboard-managed Knowledge Base documents
- **THEN** the hosted conversation may use those dashboard-managed documents without the application attaching or inspecting local documents

#### Scenario: A disabled domain does not block token issuance
- **WHEN** a session requests hosted access while the selected RAG or Agent Skill synchronization domain is disabled
- **THEN** token issuance and hosted workflow validation proceed normally and do not report the disabled domain as an application synchronization failure
