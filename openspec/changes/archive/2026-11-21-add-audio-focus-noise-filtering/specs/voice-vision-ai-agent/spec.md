# Spec Delta

## ADDED Requirements

### Requirement: Realtime near-field input noise reduction alignment
When the active agent pipeline mode is `realtime`, the system SHALL configure upstream near-field input audio noise reduction whenever Audio Focus is enabled and SHALL omit near-field noise reduction when Audio Focus is disabled.

#### Scenario: Realtime session starts with Audio Focus enabled
- **WHEN** a `realtime` session initializes while Audio Focus is enabled
- **THEN** the `session.update` payload sent to the Realtime provider includes `input_audio_noise_reduction` configured for `near_field` speaker isolation

#### Scenario: Realtime session starts with Audio Focus disabled
- **WHEN** a `realtime` session initializes while Audio Focus is disabled
- **THEN** the `session.update` payload omits `near_field` input noise reduction
