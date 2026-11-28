# Spec Delta

## MODIFIED Requirements

### Requirement: Synchronized and dashboard knowledge grounding
Hosted sessions SHALL ground document answers using the configured agent's attached ElevenLabs Knowledge Base and RAG index, combining locally `ready` application documents synchronized server-side while application RAG synchronization is enabled with any additional unmanaged documents attached in the ElevenLabs dashboard. During a hosted voice call, the application SHALL NOT query its local pgvector store at turn time.

#### Scenario: Synchronized local or dashboard policy answers a question
- **WHEN** a local `ready` policy document has been synchronized and indexed on the configured hosted agent (alongside any dashboard-attached documents) and the user asks a question covered by that policy
- **THEN** the spoken answer reflects the attached ElevenLabs Knowledge Base without calling the application's pgvector store during the turn

#### Scenario: Unmanaged dashboard documents preserved alongside synced local documents
- **WHEN** the configured hosted agent also has a document attached directly in the ElevenLabs dashboard that is not tracked by the application
- **THEN** synchronizing local documents preserves that dashboard document on the agent alongside the application-managed documents

#### Scenario: Administrator syncs a single ready local document
- **WHEN** application RAG synchronization is enabled and an administrator requests sync or retry for one locally `ready` document
- **THEN** the server uploads or updates only that tracked document, verifies its hosted attachment and index state, and leaves other local and dashboard-managed entries unchanged

#### Scenario: Application RAG synchronization is disabled
- **WHEN** an administrator requests global or per-document application knowledge synchronization while the saved application RAG synchronization setting is disabled
- **THEN** the server does not upload, attach, or index local documents at ElevenLabs, while dashboard-managed hosted knowledge remains available to the conversation
