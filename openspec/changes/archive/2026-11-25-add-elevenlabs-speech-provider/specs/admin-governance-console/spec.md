# Spec Delta

## ADDED Requirements

### Requirement: Paired cascaded speech provider selection
The system SHALL allow administrators to select the existing OpenAI-compatible provider or ElevenLabs for both speech-to-text and text-to-speech in cascaded mode. The selection SHALL apply to new sessions and SHALL NOT change the configured language model.

#### Scenario: Existing installation remains local
- **WHEN** an installation is upgraded without changing its speech provider
- **THEN** new cascaded sessions continue using its existing speech endpoints, models, credentials, and voices

#### Scenario: Administrator selects ElevenLabs
- **WHEN** an administrator saves ElevenLabs as the speech provider with a configured key and default-language voice
- **THEN** new cascaded sessions use ElevenLabs for both speech services and continue using the existing language model

#### Scenario: Administrator returns to the existing provider
- **WHEN** an administrator switches back to the existing provider
- **THEN** new cascaded sessions use the previously saved existing speech settings

#### Scenario: Selection is not applied to realtime
- **WHEN** a session uses realtime mode
- **THEN** the cascaded speech provider selection does not change its realtime provider

#### Scenario: Non-administrator attempts to change provider
- **WHEN** a non-administrator attempts to change the speech provider
- **THEN** the system rejects the request without changing the saved configuration

### Requirement: Separate write-only ElevenLabs credential
The system SHALL allow administrators to configure the ElevenLabs API key, store it encrypted separately from the existing provider key, and expose only a masked preview showing its last five characters after submission. Updating or omitting either key SHALL NOT replace or disclose the other key.

#### Scenario: Save and read an ElevenLabs key
- **WHEN** an administrator saves an ElevenLabs key and later reads the configuration
- **THEN** the key is encrypted at rest and only a masked preview showing its last five characters is returned

#### Scenario: Key is too short to mask safely
- **WHEN** an administrator submits a non-empty ElevenLabs key of five characters or fewer
- **THEN** the system rejects the update without revealing the key or changing the stored configuration

#### Scenario: Save unrelated configuration
- **WHEN** an administrator updates other settings without providing an ElevenLabs key
- **THEN** the existing ElevenLabs key remains unchanged

#### Scenario: Credential and configuration audit
- **WHEN** an administrator changes the speech provider or its credential
- **THEN** an audit event records the change without the key or other secret material

### Requirement: ElevenLabs configuration and validation
The system SHALL provide separate ElevenLabs speech model settings with balanced defaults and SHALL make the provider's cloud data transmission and potential charges clear before selection.

#### Scenario: ElevenLabs configuration is incomplete
- **WHEN** an administrator attempts to activate ElevenLabs without a saved or submitted key or a voice for the default language
- **THEN** the save is rejected with a clear message and the previously active provider remains unchanged

#### Scenario: Provider configuration and discovery
- **WHEN** an administrator configures ElevenLabs
- **THEN** the interface offers its model settings and its available voices when discovery succeeds, with manual voice ID entry when discovery is unavailable
