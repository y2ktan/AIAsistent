# Spec Delta

## MODIFIED Requirements

### Requirement: Real-time transcript and grounded citation drawer
The system SHALL display streaming user and assistant captions and tool-execution indicators synchronized with spoken output, SHALL display interactive non-skill RAG citation badges and excerpt previews only to authenticated administrators, and SHALL exclude Agent Skill (`SKILL.md`) definitions from home-page citation badges for every role during conversations.

#### Scenario: Grounded response displays document citation
- **WHEN** an authenticated administrator receives an assistant answer using retrieved passages from standard knowledge documents
- **THEN** the home page displays clickable source badges showing the matched document title and relevant excerpt preview

#### Scenario: Non-admin user does not see grounded document citations
- **WHEN** an authenticated user whose role is not `admin` receives an assistant answer using retrieved passages from standard knowledge documents
- **THEN** the home page omits all standard RAG citation badges and expandable excerpt previews while preserving the transcript, assistant response, tool indicators, and conversation controls

#### Scenario: Agent skill entries omitted from home-page citations
- **WHEN** a citation event during an ongoing conversation includes Agent Skill (`SKILL.md`) chunks—either alone or alongside standard knowledge document chunks
- **THEN** the home page omits the Agent Skill citation badges for administrators and non-admin users, renders only administrator-visible non-skill citations, and renders no citation section for a non-admin user

#### Scenario: Admin navigation surfaced only to administrators
- **WHEN** an authenticated user with role `admin` views the home page
- **THEN** a navigation link to `/admin` is rendered, whereas users with role `user` do not see administrative navigation controls
