# AIAsistent

A local browser-based AI assistant with multi-turn chat, retrieval-augmented
generation (RAG) over your text/Markdown documents, and optional MCP tools.
Requires **Node.js 22+**; no external packages or build step are needed.

## Run

Configure an OpenAI-compatible chat-completions provider on the server:

```sh
export AI_API_KEY='<your-provider-key>'
export AI_MODEL='gpt-4o-mini'
export AI_BASE_URL='https://api.openai.com/v1'
npm start
```

Open **http://127.0.0.1:3000**. `PORT` overrides the port. Credentials are read
only from the server environment, never sent to the browser. Environment files
are not loaded automatically; `.env` is ignored by Git to avoid accidental commits.
The UI and document uploads work without a key, but chat requires `AI_API_KEY`.
Provider usage may incur charges; conversations and retrieved excerpts are sent
to the configured provider.

## Documents

Upload `.txt` or `.md` files in the Documents panel, then ask a question about
their contents. The server chunks the text and ranks passages using
inverse-document-frequency-weighted keyword overlap. Up to four matching
passages are included in each request. The assistant is instructed to cite
them as `[1]`, `[2]`, etc.; expand **Retrieved sources** to inspect the actual
excerpts and source line ranges. Verify AI answers against those excerpts.

Limits: 1 MiB per document, 20 documents, 2 MiB per API request. Remove documents
with **Remove**. Documents exist only in server memory and disappear on restart.
Chat history exists only in the current browser page; **New chat** clears it.
The most recent 19 exchanges are retained as context.

This lightweight RAG uses lexical retrieval, not embeddings: questions need
words in common with the documents. There is no PDF/DOCX parsing, OCR, persistent
vector database, or semantic synonym matching.

## MCP tools

Configure trusted **Streamable HTTP** MCP servers using `MCP_SERVERS`, a JSON
array of up to five entries. Each entry requires `name`, `url`, and an explicit
`allowedTools` list of the server's tool names. An optional `token` supplies
server-side bearer authentication.

```sh
export MCP_SERVERS='[{"name":"local","url":"http://127.0.0.1:8000/mcp","allowedTools":["search"]}]'
npm start
```

Use **Allow configured MCP tools for this message** to authorize AI-selected
calls to the allowlisted tools. Tools are off initially and after **New chat**.
The setting remains selected for subsequent messages until unchecked. The UI
lists permitted tools and shows which were used in each answer.

The client initializes MCP, negotiates a supported protocol version, lists tools
with pagination, and processes JSON or SSE responses, forwarding tool results
to the AI for its final answer. Session IDs are honored and sessions are closed
after each chat request. Tool rounds, response sizes, and network timeouts are
bounded. Tool argument objects are passed to the MCP server, which is responsible
for validating its input schemas.

Only Streamable HTTP is supported; stdio and legacy HTTP+SSE transports, MCP
resources/prompts, server-initiated sampling/elicitation, and session resumption
are not supported. Use an HTTP bridge if your MCP server is stdio-only.

## Safety and scope

- This is a **single-user local app**, bound to `127.0.0.1`. Do not expose it
  through a public proxy: it has no user authentication or tenant isolation.
  Host/Origin validation and a required custom API header protect against
  cross-origin browser requests and DNS rebinding.
- Endpoint URLs are trusted administrator configuration, not browser inputs.
  Redirects are disabled. Only configure endpoints you trust with your data.
- MCP tools can have side effects. Allowlist only necessary tools, preferably
  read-only ones. Enabling tools authorizes execution without a separate
  per-call confirmation. Prompt-injection defenses are not a guarantee; do not
  enable privileged tools when using untrusted documents or tool outputs.
- Uploaded content, model output, and tool names are displayed as plain text,
  not executable HTML. Provider and MCP error bodies are not exposed.
- Requests are not streamed to the browser. A request allows up to six AI rounds
  and eight tool calls per round, with 60-second AI and 30-second MCP timeouts.
  Only one chat request runs at a time.

## Tests

```sh
npm test
```

Tests use Node's built-in test runner and local mock AI/MCP servers; no
credentials or external network services are required. They cover retrieval,
chat context, MCP initialization/SSE/session handling, tool permissions and
limits, validation, and local API security boundaries.