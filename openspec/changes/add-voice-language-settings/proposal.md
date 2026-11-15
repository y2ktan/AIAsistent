# Proposal

## Why

The assistant answers in English whatever language it is spoken to, and it has a single fixed voice. In testing, a Mandarin question got an English answer, and Mandarin text sent to the configured English voice produced about 18 seconds of audio for a 4-second sentence. The people using it speak English, Mandarin, Cantonese, and Malay, often mixed, and administrators have no way to say which languages the assistant should handle or how it should sound in each. On top of that, the assistant's spoken replies cannot currently be heard at all, so none of this can be judged by ear.

## What Changes

- Add a supported voice languages setting to the AI Agent area of `/admin`, where an administrator can add a language, remove a language, assign a voice to each language, and choose one default language.
- Start with English, Mandarin Chinese, Cantonese, and Malay, with English as the default. Hokkien is deferred.
- Make the assistant reply, in transcript text and in speech, in the language the user just spoke, when that language is supported and has a voice.
- Make the spoken reply use the voice assigned to the language of the reply.
- When the user speaks a language that is not supported, or is supported but has no voice, the assistant interprets it as well as it can, replies in the default language, and tells the user once per session that it cannot reply in their language.
- Make spoken replies audible. Exploration traced the silence to two defects: the server forwards a full second of synthesized audio as a single 20 ms frame, and the web client pauses playback at the start of every turn and never resumes it. Neither has been confirmed by ear yet. Both are defects against the existing `voice-vision-ai-agent` requirement to stream synthesized speech, so they need no new requirement.

Out of scope:

- Hokkien, in any form.
- Choosing or installing a speech engine that can speak Cantonese or Malay. With the speech engine configured today, only English and Mandarin have voices, so Cantonese and Malay will use the fallback behaviour until an administrator assigns them a voice from an engine that has one.
- Rewording replies for speech (removing markdown, lists, and emoji) and giving the assistant memory of earlier turns. Both came up in exploration and are not part of this change.

## Capabilities

### New Capabilities

- `voice-language-support`: which spoken languages the assistant handles, how administrators manage that list and its voices, and how the assistant chooses the language and voice of each reply.

### Modified Capabilities

None. The existing `admin-governance-console` audit requirement already covers AI configuration updates. The existing `voice-vision-ai-agent` requirements are unchanged: making speech audible restores behaviour they already require.

## Impact

- **Stored configuration**: a new stored list of supported languages, a voice per language, and a default language. Today the configuration holds one voice, which the admin page does not expose.
- **Speech streaming and playback**: the server path that forwards synthesized audio to the browser, and the web client's handling of interruptions.
- **Admin API and `/admin` page**: new read and update operations for the language settings, and a new control in the AI Agent tab.
- **Cascaded pipeline**: speech-to-text must report the language it heard, the language model must be told which language to answer in, and speech synthesis must pick a voice per reply.
- **Realtime mode**: the provider chooses the voice, so per-language voices may not apply there. How the reply-language and fallback rules are met in realtime mode is a design question.
- **Models**: the language model in use during exploration answered Mandarin poorly even when instructed, so a stronger model is likely needed for acceptable Mandarin, Cantonese, and Malay replies.
