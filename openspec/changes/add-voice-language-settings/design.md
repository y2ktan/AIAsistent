# Design

## Context

See `proposal.md` for motivation. The facts below were read from the code or measured against the running speech services; where something is inferred or comes from general model knowledge, it says so.

**How a spoken turn works today (cascaded mode).** `ProcessTurn` in `server/internal/agent/cascaded.go` runs speech-to-text, retrieval, the language model, then speech synthesis.

- The speech-to-text response is read for `text` only. Measured: the local Whisper service also returns `language` (for example `"zh"`) when asked for `response_format=verbose_json`.
- The language model receives the system prompt and the current utterance. Nothing tells it which language to answer in, and nothing is remembered between turns.
- Speech synthesis uses one voice, `agent_configs.tts_voice`. The admin page has no input for it; the page sends the stored value back unchanged on every save.

**Why speech is silent.** Two causes, both inferred from the audio stream and the code, neither confirmed by ear:

1. The speech service returns Ogg pages holding many Opus packets each (measured: 50 packets of 20 ms per page). `synthesizeAndStream` forwards each page as one 20 ms sample. The Ogg reader in use returns whole pages and does not expose where packets start and end.
2. The web client pauses the audio element on every `barge_in` event and never resumes it. The server sends `barge_in` at the start of every turn.

**Constraints that shape the design.**

- Every `*.up.sql` file is executed again on each server start; there is no record of which migrations have run. New migrations must be safe to repeat.
- Admin routes share one wrapper (authenticated, CSRF-checked, admin role), request bodies are decoded strictly (unknown fields are rejected), and an audit helper already exists.
- `setup-local-ai.sh` writes `agent_configs` directly with SQL, naming its columns.
- Realtime mode is not usable end to end today: the realtime adapter is never fed microphone audio (its audio-input method has no caller) and it sets no voice.
- Go is not installed on the host. The Go suite runs in the `golang:1.25-bookworm` image with the source mounted read-only, and passes today (7 packages). The web suite runs with `npm test` under jsdom and passes today (6 tests). There is no React component-testing library.

## Goals / Non-Goals

**Goals:**

- The server decides the reply language and the voice for each turn with plain logic that can be unit-tested without a language model.
- Language settings are stored and saved independently of the rest of the AI configuration, so neither can overwrite the other.
- Synthesized speech reaches the browser as correctly timed Opus packets and is not left paused.

**Non-Goals:**

- Feeding microphone audio to realtime mode, or per-language voices in realtime mode.
- Checking a typed voice name against the speech engine's voice list.
- Changing voice part-way through a single reply.

## Decisions

### 1. Language settings get their own table

A new `voice_languages` table holds one row per supported language: language code (primary key), voice, a default flag, and a display position. A unique partial index allows at most one default row. Saving replaces all rows in one transaction.

An empty table means "never saved". In that case the server computes the initial settings instead of reading them: English, Mandarin Chinese, Cantonese, and Malay, with English as the default. English takes the existing `agent_configs.tts_voice` as its voice (or the code default when that is blank), which satisfies the rule that the default language must have a voice. The other three start with no voice.

After language settings have been saved, the speech path no longer reads `tts_voice`. The column and its API field stay, so the existing page payload and `setup-local-ai.sh` keep working.

*Alternative considered:* a JSON column on `agent_configs`. Rejected because saving AI configuration rewrites that row as a whole, the row does not exist until AI configuration is first saved, and the database could not enforce unique codes or a single default.

### 2. One read and one whole-list save endpoint

- `GET /api/v1/admin/voice-languages` returns the current languages (code, display name, voice), the default language, whether settings have ever been saved, and the catalog of languages that can be added.
- `PUT /api/v1/admin/voice-languages` replaces the list and default together.

Both sit behind the existing admin wrapper, which gives the non-administrator refusal for free. The save is rejected with a `400` and a readable message when the list is empty, a code is not in the catalog, a code appears twice, the default is not in the list ("choose another default language before removing this one"), or the default has no voice. A rejected save changes nothing. A successful save writes an audit entry (`admin.voice_languages.update`) listing the codes added and removed and the default.

*Alternative considered:* separate add, remove, and update endpoints. Rejected because every spec scenario is "edit, then save", and swapping the default while removing the old one would need the calls made in a particular order. The cost is that two administrators saving at the same time overwrite each other (last save wins), the same as the existing AI configuration save.

### 3. A fixed catalog of recognised languages on the server

The catalog lives in the `agent` package, which both the admin API and the turn logic already depend on. Each entry has a code, an English display name, a script family (Latin, Han, or other), and the labels speech-to-text may use for it. Codes follow the labels Whisper emits (`en`, `zh`, `yue`, `ms`, and so on), because speech-to-text is what identifies the language. The first version lists English, Mandarin Chinese, Cantonese, Malay, Indonesian, Tamil, Japanese, Korean, Hindi, Thai, Vietnamese, Spanish, French, German, Portuguese, Italian, and Arabic. Hokkien is not in the catalog, so it cannot be added.

The read endpoint returns the catalog, so the web page does not keep its own copy.

*Alternative considered:* letting administrators type any code. Rejected because a mistyped code would be accepted and then never match anything speech-to-text reports.

### 4. The utterance language comes from speech-to-text, checked against the transcript

1. Ask speech-to-text for `verbose_json` and read `language`. Normalise it through the catalog, which accepts codes and names in any letter case (the measured local service returns `zh`; some providers return names such as `english`).
2. If the speech-to-text service rejects `verbose_json` with a `400`, retry the request in the plain format and stop asking for `verbose_json` for the rest of the session. This is the same degrade-and-remember pattern the turn logic already uses when a model rejects tool definitions.
3. Count letters in the transcript by script. When the dominant script contradicts the reported language (for example the label says English but the text is mostly Han characters), the script wins. This is what makes a mixed Mandarin and English sentence follow whichever language makes up most of it.
4. Treat the language as undetermined when there is no usable label and the script is inconclusive, or when the transcript has fewer than two word units (a Latin-script word or a single Han character each). An undetermined utterance keeps the language of the previous reply in the session, or the default language if there is none.

One alias: a label of Indonesian is treated as Malay when Malay is supported and Indonesian is not. Whisper is known to confuse the two; this was not measured here.

*Alternative considered:* asking the language model to state the language. Rejected because voice selection would then depend on a model following a formatting instruction, and the local model tested answered a Mandarin question in English when left to itself.

### 5. The server tells the model which language to answer in

A pure function takes the detected language, the session's language settings, and the session state, and returns the reply language, whether this is a fallback, and whether the notice is due. The session state is the language of the last reply and the set of languages the assistant has already said it cannot speak.

- Detected language is supported and has a voice: the reply language is that language.
- Otherwise: the reply language is the default language. The notice is due only if this language is not yet in the "already said" set, and it is then added.

The turn adds one short system message for the model: "reply only in X", or for a fallback "the user spoke Y; understand it as well as you can, answer in X", plus "tell them in one short sentence that you cannot reply in Y yet" when the notice is due. A language speech-to-text names that is not in the catalog is described as "that language".

Language settings are read once when a session starts and held for that session, so a save applies to sessions started afterwards.

*Alternative considered:* having the server write the notice sentence itself. Rejected because the notice must be in the default language, which can be any catalog language, and that would mean keeping a translated sentence for all of them.

### 6. The voice follows the reply, with a script guard

The reply is spoken with the voice assigned to the reply language. Before synthesis, the server checks the script of the reply text. If it contradicts the reply language (the model answered in English although told to answer in Mandarin), the server uses the voice of a supported, voiced language whose script matches, preferring the default language, and records that language as the last reply language.

The guard exists because of a measured failure: Mandarin text sent to an English voice produced 18 seconds of audio for a 4-second sentence.

### 7. Realtime mode gets the rules as instructions only

The realtime session instructions gain a short language policy: the languages the assistant may reply in (supported with a voice), the default language, and the fallback and once-only notice rules. Per-language voices do not apply; the spec limits that requirement to cascaded mode. Because realtime mode does not receive audio today, this can be checked only by asserting the instructions sent.

### 8. Speech is split into packets by this project's own Ogg reader

The speech path stops using the page-level reader. A small reader in `server/internal/agent/ogg.go`, next to the Ogg writer already there, walks each page's segment table and yields one Opus packet at a time, including packets that continue across a page boundary. Each packet's duration is read from its Opus header byte instead of assuming 20 ms. Packets are released against a running clock (start time plus the audio sent so far), not by sleeping a fixed time after each one, so delivery does not drift behind real time.

On the web client, a `barge_in` event no longer pauses the audio element. The server stops sending audio when a turn is interrupted, so playback goes quiet without the client pausing.

*Alternatives considered:* asking the speech service for raw audio and encoding Opus on the server was rejected because it needs an Opus encoder and the server is built without C libraries. Keeping the existing reader was not possible because it does not expose packet boundaries.

## Risks / Trade-offs

- [The model ignores the language instruction or leaves out the notice] → The script guard keeps the voice matched to the text. The notice is still marked as given. The proposal already notes that a stronger model than the one tested is likely needed. Task 7.2 checks behaviour with the real model.
- [A third cause of silence exists beyond the two identified] → Task 1.5 checks by ear and names the exact browser statistics to read if speech is still silent.
- [Speech-to-text cannot label Cantonese] → From model knowledge, not measured here: small Whisper models have no Cantonese label and report Mandarin. Cantonese would then be answered in Mandarin with no notice. Task 7.2 records what the configured model returns for Cantonese and Malay.
- [Speech-to-text returns no language at all] → Latin-script languages cannot be told apart from text alone, so the assistant stays in the previous or default language. Han-script text is still recognised as Chinese.
- [Two languages in the same script are mixed] → English with Malay, or Mandarin with Cantonese, follow the speech-to-text label alone and may not match "most of the utterance".
- [A voice name is mistyped] → Voice names are free text, as the single voice is today. A wrong name fails at speaking time with the existing "Speech synthesis failed" message.
- [One voice reads a reply containing words from another language] → Quality is unknown and depends on the speech engine.
- [Two administrators save at once] → Last save wins.
- [The migration runs on every start] → It uses only repeat-safe statements, and task 3.1 restarts the server to prove it.

## Migration Plan

1. Add `000002_voice_languages` (up and down). The up file creates the table and index only if they do not exist. No data is copied; initial settings are computed while the table is empty.
2. Rebuild and restart the server and web containers.
3. `setup-local-ai.sh` gains one step for the local speech engine: when the table is empty, it saves English (default, existing voice) and Mandarin Chinese with the local engine's Mandarin voice, plus Cantonese and Malay with no voice. It does not touch a table that already has rows, so administrator changes survive a re-run.

Rollback: redeploy the previous images. The earlier code ignores the new table and still reads `tts_voice`, which this change leaves untouched. The down migration drops the table.

## Open Questions

- Which speech-to-text model to recommend for Cantonese, once task 7.2 shows what the current one returns.
- Whether the admin page should offer a "play sample" button for a voice. Not needed for this change.
