# Tasks

Go is not installed on the host. Run the Go suite from the project root with:

```bash
docker run --rm -v "$PWD/server":/src:ro -w /src -e GOCACHE=/tmp/gocache -e GOFLAGS=-buildvcs=false golang:1.25-bookworm go test ./...
```

Run the web suite with `npm test` inside `web/`. Both pass before this change (7 Go packages, 6 web tests). "The stack" means the containers started by `./setup-local-ai.sh`.

## 1. Make spoken replies audible

- [x] 1.1 Add an Ogg packet reader to `server/internal/agent/ogg.go` that walks each page's segment table and yields one Opus packet at a time, skipping the two header packets and joining packets that continue across a page boundary. Verify with a unit test that feeds a stream of three pages holding several packets each, one of them spanning a page boundary, and gets back exactly the original packets in order.
- [x] 1.2 Add a function that returns an Opus packet's duration from its first byte. Verify with a table-driven test covering 2.5, 5, 10, 20, 40, and 60 ms frame sizes and one-, two-, and multi-frame packets.
- [x] 1.3 Change `synthesizeAndStream` in `server/internal/agent/cascaded.go` to use the packet reader, pass each packet's own duration, and release packets against a running clock instead of a fixed sleep; remove the page-level reader import. Make the test sink record durations. Verify with a test whose fake speech service returns two pages of five 20 ms packets: the sink receives ten samples of 20 ms each and the call takes roughly 200 ms.
- [x] 1.4 Stop pausing the audio element on `barge_in` in `web/src/hooks/useVoiceAgentRTC.ts`. Verify that `npm test` still passes and that the hook no longer calls `pause()` anywhere.
- [ ] 1.5 Check by ear (needs you to speak). Start the stack, start a call, ask two questions in English one after the other, and confirm both replies are heard at normal speed. If either is silent, open `chrome://webrtc-internals`, find the inbound audio stream, and record `packetsReceived` and `totalAudioEnergy` before and after a reply: packets not increasing points at the server, packets increasing with no energy points at decoding, both increasing points at playback in the page.

## 2. Language rules

- [x] 2.1 Create `server/internal/agent/language.go` with the language catalog (code, display name, script family, speech-to-text labels), the language settings type, the initial settings (English as default with a given voice; Mandarin Chinese, Cantonese, and Malay with no voice), and validation. Verify with table-driven tests that validation rejects an empty list, an unknown code, a repeated code, a default that is not in the list, and a default with no voice, and that Hokkien is not in the catalog.
- [x] 2.2 Add speech-to-text label normalisation (codes and names, any letter case, Indonesian treated as Malay only when Malay is supported and Indonesian is not), dominant-script detection, and word-unit counting. Verify with tests for `zh`, `Chinese`, `english`, an unknown label, a mostly-Han sentence containing English words, a mostly-English sentence containing Han characters, and one-word inputs.
- [x] 2.3 Add the function that resolves the reply language, the fallback flag, and whether the notice is due, from the detected language, the settings, and the session state. Verify with one test per spec scenario: supported language with a voice, language switch, mixed utterance, undetermined utterance with and without a previous reply, supported language without a voice, unsupported language, notice not repeated in a session, and notice given again with fresh session state.

## 3. Storage and admin API

- [x] 3.1 Add `db/migrations/000002_voice_languages.up.sql` and `.down.sql` creating the `voice_languages` table and the single-default unique index with repeat-safe statements only. Verify by restarting the server container twice with the stack running: it starts cleanly both times and `\d voice_languages` in `psql` shows the table and the index.
- [x] 3.2 Add read and replace-all methods for language settings to the admin store interface in `server/internal/api/store.go`, implement them in `pg_store.go` with the replace done in one transaction, and add them to the mock store in `api_test.go`. Verify that the Go suite compiles and passes.
- [x] 3.3 Add `GET` and `PUT /api/v1/admin/voice-languages` handlers and routes behind the existing admin wrapper, returning the initial settings (English voice taken from the stored single voice) while nothing has been saved, and writing an `admin.voice_languages.update` audit entry on a successful save. Verify with API tests: the first read shows the four initial languages with English as default; adding and removing a language persist; a repeated code, removing the default, and a default with no voice each return `400` with a message and leave the stored list unchanged; a non-administrator gets `403`; a successful save appears in the audit log.

## 4. Cascaded turn

- [x] 4.1 Change `transcribe` to request `verbose_json`, return the reported language with the text, and on a `400` retry in the plain format and not ask for `verbose_json` again in that session. Verify with tests against a fake speech-to-text service: one that returns a language, and one that rejects `verbose_json`, where the second turn's request no longer asks for it.
- [x] 4.2 Add language settings to `agent.AgentConfig` and load them once per session in `attachSessionAgent` in `server/internal/api/handlers.go`, using the initial settings while nothing has been saved. Verify that the Go suite passes and that the existing cascaded test still passes with settings supplied.
- [x] 4.3 In `ProcessTurn`, detect the utterance language, resolve the reply language, add the language instruction as a system message, keep the session state (last reply language, languages already announced), and add the detected language, reply language, and fallback flag to the per-turn log line. Verify with tests against a fake model service: a Mandarin utterance produces a request telling the model to reply in Mandarin Chinese; a Cantonese utterance with no Cantonese voice produces a request naming the default language and asking for the notice on the first turn and not on the second.
- [x] 4.4 Choose the voice from the reply language, apply the script guard, and pass the chosen voice to `synthesizeAndStream`. Verify with tests: a Mandarin reply reaches the fake speech service with the Mandarin voice; a reply written in English when Mandarin was requested reaches it with the English voice.

## 5. Realtime instructions

- [x] 5.1 Add the language policy (languages the assistant may reply in, the default language, the fallback and once-only notice rules) to the instructions sent in `session.update` in `server/internal/agent/realtime.go`. Verify by extending the realtime test to assert the instructions contain the voiced language names and the default language.

## 6. Admin page

- [x] 6.1 Create `web/src/lib/voice-languages.ts` with the types and pure helpers the page needs: add a language, remove a language, whether a row may be removed, and which catalog languages are still available to add. Verify with new vitest tests covering add, remove, the default row not being removable, and an already-listed language not being offered.
- [x] 6.2 Add a "Voice languages" card to the AI Agent tab in `web/src/app/admin/page.tsx`, outside the existing configuration form, with its own Save button: one row per language (name, voice input, default selector, remove button disabled on the default row with an explanation), a hint on rows with no voice that replies will be in the default language, and an add-language selector. Show the server's message when a save is rejected. Verify that `npm test` passes and the web image builds.
- [x] 6.3 Add a "Voice languages" section to `README.md` covering where the setting is, the default-language rules, the fallback behaviour, and that Cantonese and Malay need a speech engine that has voices for them. Verify the section matches the behaviour in the spec.
- [x] 6.4 Check in the browser with the stack running: on `/admin`, add Japanese and save, reload and see it listed; remove it and save; confirm the default row cannot be removed; clear the default's voice, save, and see the rejection message.

## 7. Local stack and end to end

- [x] 7.1 Add a step to `setup-local-ai.sh` that, only when `voice_languages` is empty, saves English (default, existing voice), Mandarin Chinese with voice `zf_xiaoxiao`, and Cantonese and Malay with no voice. Verify with `bash -n setup-local-ai.sh`, then run it and query the table; change a voice on `/admin`, run the script again, and confirm the change is still there.
- [ ] 7.2 Check end to end by ear (needs you to speak) with the stack running: an English question gets an English reply in the English voice; a Mandarin question gets a Mandarin reply in the Mandarin voice; switching back to English switches the reply; a Cantonese or Malay question gets a reply in English with the notice once and not on the next question. Read the detected and reply languages from the server log for each turn, and record in the Risks section of `design.md` what speech-to-text reported for Cantonese and for Malay.
- [x] 7.3 Run the full Go suite and `npm test`, and run `openspec validate "add-voice-language-settings" --strict`. Verify all three pass.
