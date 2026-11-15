# voice-language-support Specification

## Purpose

Defines which spoken languages the assistant handles, how administrators manage that list and the voice used for each language, and how the assistant chooses the language and voice of each reply.

## Requirements

### Requirement: Administrator-managed supported voice languages
The system SHALL let administrators on `/admin` view, add, and remove the languages the assistant supports for voice conversation, and SHALL apply the saved list to sessions started after the change.

#### Scenario: Administrator adds a language
- **WHEN** an administrator adds a recognised language that is not already in the list and saves
- **THEN** the language appears in the supported list, applies to sessions started afterwards, and an audit entry records the change

#### Scenario: Administrator removes a language
- **WHEN** an administrator removes a supported language that is not the default language and saves
- **THEN** the language no longer appears in the list, later sessions treat it as unsupported, and an audit entry records the change

#### Scenario: Duplicate language rejected
- **WHEN** an administrator attempts to add a language that is already in the supported list
- **THEN** the system rejects the request with a validation message and leaves the list unchanged

#### Scenario: Non-administrator cannot change language settings
- **WHEN** an authenticated user with role `user` attempts to add, remove, or edit a supported language
- **THEN** the system refuses the request and the language settings are unchanged

### Requirement: Initial supported languages
On an installation with no saved language settings, the system SHALL start with English, Mandarin Chinese, Cantonese, and Malay as the supported languages, with English as the default language.

#### Scenario: Language settings opened before any have been saved
- **WHEN** an administrator opens the language settings on an installation where none have been saved
- **THEN** the list shows English, Mandarin Chinese, Cantonese, and Malay, with English marked as the default

### Requirement: Default reply language
The system SHALL require exactly one supported language with an assigned voice to be designated the default language, used whenever the assistant cannot reply in the language the user spoke.

#### Scenario: Administrator changes the default language
- **WHEN** an administrator designates a different supported language that has an assigned voice as the default and saves
- **THEN** later sessions use that language for fallback replies

#### Scenario: Removing the default language is prevented
- **WHEN** an administrator attempts to remove the language currently designated as the default
- **THEN** the system rejects the request and explains that another default must be chosen first

#### Scenario: Language without a voice cannot be the default
- **WHEN** an administrator attempts to designate a language with no assigned voice as the default, or to clear the voice of the current default language
- **THEN** the system rejects the request and the default language keeps its voice

### Requirement: Per-language voice assignment
The system SHALL let administrators assign a speech voice to each supported language, and SHALL treat a supported language with no assigned voice as understood but not spoken.

#### Scenario: Administrator assigns a voice to a language
- **WHEN** an administrator sets a voice for a supported language and saves
- **THEN** sessions started afterwards speak replies in that language with that voice

#### Scenario: Language saved without a voice
- **WHEN** an administrator adds a language and leaves its voice empty
- **THEN** the language is saved and the settings view indicates that the assistant will answer that language in the default language

### Requirement: Replies follow the user's spoken language
The system SHALL reply, in both transcript text and speech, in the language of the user's most recent utterance when that language is supported and has an assigned voice, and SHALL prefer the configured default language when disambiguating ambiguous same-script utterances or falling back when no language is detected on an initial turn.

#### Scenario: User speaks a supported language
- **WHEN** Mandarin Chinese is supported with an assigned voice and the user asks a question in Mandarin
- **THEN** the assistant's transcript reply is in Mandarin and the reply is spoken in Mandarin

#### Scenario: User switches language during a conversation
- **WHEN** a user who has been speaking English asks their next question in another supported language that has an assigned voice
- **THEN** the assistant replies to that question in the new language

#### Scenario: Utterance mixes two supported languages
- **WHEN** a single utterance mixes two supported languages
- **THEN** the assistant replies in the language that makes up most of the utterance

#### Scenario: Ambiguous same-script utterance prefers configured default language
- **WHEN** the dominant script of an utterance matches multiple configured languages and speech-to-text does not provide a matching language label, while the configured default language belongs to that script family
- **THEN** the assistant resolves the utterance to the configured default language

#### Scenario: Language of an utterance cannot be determined
- **WHEN** the language of an utterance cannot be determined, such as a one-word acknowledgement or unclear audio
- **THEN** the assistant replies in the language of its previous reply in that session, or in the configured default language (such as Mandarin Chinese when set as default) if there is no previous reply

### Requirement: Spoken replies use the voice of the reply language
In `cascaded` mode, the system SHALL synthesize each spoken reply with the voice assigned to the language the reply is written in.

#### Scenario: Reply spoken with the matching voice
- **WHEN** the assistant produces a reply in Mandarin while voices are assigned to both English and Mandarin
- **THEN** the reply audio is produced with the voice assigned to Mandarin and not the voice assigned to English

### Requirement: Fallback for languages the assistant cannot speak
The system SHALL, when the user speaks a language that is unsupported or has no assigned voice, interpret the utterance as well as it can and reply in the default language. It SHALL tell the user that it cannot reply in that language the first time this happens for that language in a session, and SHALL NOT repeat the notice for the same language later in that session.

#### Scenario: Supported language without a voice
- **WHEN** Cantonese is supported with no assigned voice and the user asks a question in Cantonese for the first time in a session
- **THEN** the assistant answers the question in the default language and tells the user it cannot yet speak Cantonese

#### Scenario: Unsupported language
- **WHEN** the user speaks a language that is not in the supported list for the first time in a session
- **THEN** the assistant replies in the default language and tells the user it cannot reply in that language

#### Scenario: Notice is not repeated within a session
- **WHEN** the user keeps speaking a language the assistant has already said it cannot reply in during the same session
- **THEN** the assistant answers in the default language without repeating the notice

#### Scenario: Notice is given again in a new session
- **WHEN** the user starts a new session and speaks a language the assistant cannot reply in
- **THEN** the assistant again tells the user it cannot reply in that language
