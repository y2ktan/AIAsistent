#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

LLM_MODEL="${OLLAMA_LLM_MODEL:-llama3:latest}"
EMBED_MODEL="${OLLAMA_EMBED_MODEL:-nomic-embed-text:latest}"
EMBED_DIM="${OLLAMA_EMBED_DIM:-768}"
STT_MODEL="${LOCAL_STT_MODEL:-Systran/faster-whisper-base}"
TTS_MODEL="${LOCAL_TTS_MODEL:-kokoro}"
TTS_VOICE="${LOCAL_TTS_VOICE:-af_heart}"

echo "==> Checking prerequisites..."
command -v docker >/dev/null 2>&1 || { echo "Error: docker is not installed."; exit 1; }
command -v ollama >/dev/null 2>&1 || { echo "Error: ollama is not installed."; exit 1; }
docker info >/dev/null 2>&1 || { echo "Error: Docker daemon is not running."; exit 1; }

if [ ! -f .env ]; then
  echo "Error: .env file not found in $ROOT_DIR."
  exit 1
fi

echo "==> Ensuring Ollama is running..."
if ! curl -fsS "http://127.0.0.1:11434/api/tags" >/dev/null 2>&1; then
  echo "Starting ollama serve in background (listening on 0.0.0.0:11434)..."
  OLLAMA_HOST="0.0.0.0:11434" nohup ollama serve >/tmp/ollama-serve.log 2>&1 &
  for i in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:11434/api/tags" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done
fi
curl -fsS "http://127.0.0.1:11434/api/tags" >/dev/null 2>&1 || {
  echo "Error: Ollama failed to start. Check /tmp/ollama-serve.log"
  exit 1
}

ensure_ollama_model() {
  local model="$1"
  if ! ollama list | awk 'NR>1 {print $1}' | grep -Fxq "$model"; then
    echo "==> Pulling Ollama model: $model"
    ollama pull "$model"
  else
    echo "==> Ollama model already present: $model"
  fi
}

ensure_ollama_model "$LLM_MODEL"
ensure_ollama_model "$EMBED_MODEL"

echo "==> Enabling ALLOW_PRIVATE_AI_ENDPOINTS=true in .env..."
if grep -q '^ALLOW_PRIVATE_AI_ENDPOINTS=' .env; then
  sed -i.bak 's/^ALLOW_PRIVATE_AI_ENDPOINTS=.*/ALLOW_PRIVATE_AI_ENDPOINTS=true/' .env && rm -f .env.bak
else
  printf '\nALLOW_PRIVATE_AI_ENDPOINTS=true\n' >> .env
fi

echo "==> Starting Docker Compose stack (app + local STT/TTS)..."
docker compose -f docker-compose.yml -f docker-compose.local-ai.yml up -d --build

echo "==> Waiting for PostgreSQL to be healthy..."
for i in $(seq 1 30); do
  if docker compose -f docker-compose.yml -f docker-compose.local-ai.yml exec -T postgres \
    sh -c 'pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

echo "==> Configuring pgvector dimension ($EMBED_DIM) and default agent_configs..."
docker compose -f docker-compose.yml -f docker-compose.local-ai.yml exec -T postgres \
  sh -c 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<SQL
DO \$\$
BEGIN
  IF (SELECT COUNT(*) FROM document_chunks) = 0 THEN
    DROP INDEX IF EXISTS idx_document_chunks_embedding_hnsw;
    ALTER TABLE document_chunks ALTER COLUMN embedding TYPE vector(${EMBED_DIM});
    CREATE INDEX idx_document_chunks_embedding_hnsw
      ON document_chunks USING hnsw (embedding vector_cosine_ops);
  END IF;
END
\$\$;

INSERT INTO agent_configs (
  name,
  mode,
  stt_base_url,
  stt_model,
  llm_base_url,
  llm_model,
  tts_base_url,
  tts_model,
  tts_voice,
  embedding_base_url,
  embedding_model,
  embedding_dim
)
VALUES (
  'default',
  'cascaded',
  'http://stt:8000/v1',
  '${STT_MODEL}',
  'http://host.docker.internal:11434/v1',
  '${LLM_MODEL}',
  'http://tts:8880/v1',
  '${TTS_MODEL}',
  '${TTS_VOICE}',
  'http://host.docker.internal:11434/v1',
  '${EMBED_MODEL}',
  ${EMBED_DIM}
)
ON CONFLICT (name) DO UPDATE SET
  mode = EXCLUDED.mode,
  stt_base_url = EXCLUDED.stt_base_url,
  stt_model = EXCLUDED.stt_model,
  llm_base_url = EXCLUDED.llm_base_url,
  llm_model = EXCLUDED.llm_model,
  tts_base_url = EXCLUDED.tts_base_url,
  tts_model = EXCLUDED.tts_model,
  tts_voice = EXCLUDED.tts_voice,
  embedding_base_url = EXCLUDED.embedding_base_url,
  embedding_model = EXCLUDED.embedding_model,
  embedding_dim = EXCLUDED.embedding_dim,
  updated_at = NOW();

INSERT INTO voice_languages (code, name, voice, is_default, position)
SELECT v.code, v.name, v.voice, v.is_default, v.position
FROM (VALUES
  ('en',  'English',          '${TTS_VOICE}', TRUE,  0),
  ('zh',  'Mandarin Chinese', 'zf_xiaoxiao',  FALSE, 1),
  ('yue', 'Cantonese',        '',             FALSE, 2),
  ('ms',  'Malay',            '',             FALSE, 3)
) AS v(code, name, voice, is_default, position)
WHERE NOT EXISTS (SELECT 1 FROM voice_languages);
SQL

echo "==> Warming up local STT model (first load can take ~1 minute on CPU)..."
WARMUP_WAV="$(mktemp -t aegis-warmup).wav"
# Minimal 16kHz mono 16-bit PCM WAV header + 0.5s of silence
printf 'RIFF\xa4\x3e\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x80\x3e\x00\x00\x00\x7d\x00\x00\x02\x00\x10\x00data\x80\x3e\x00\x00' > "$WARMUP_WAV"
head -c 16000 /dev/zero >> "$WARMUP_WAV"
for i in $(seq 1 30); do
  if docker compose -f docker-compose.yml -f docker-compose.local-ai.yml exec -T server \
      curl -fsS -m 300 -F "model=${STT_MODEL}" -F "file=@-;filename=warmup.wav;type=audio/wav" \
      http://stt:8000/v1/audio/transcriptions < "$WARMUP_WAV" >/dev/null 2>&1; then
    echo "    STT model ready."
    break
  fi
  sleep 5
done
rm -f "$WARMUP_WAV"

echo ""
echo "Local AI setup complete!"
echo "  Web UI:         http://localhost:3000"
echo "  Admin Console:  http://localhost:3000/admin"
echo "  LLM (Ollama):   http://host.docker.internal:11434/v1 ($LLM_MODEL)"
echo "  Embed (Ollama): http://host.docker.internal:11434/v1 ($EMBED_MODEL, ${EMBED_DIM}d)"
echo "  STT (Docker):   http://stt:8000/v1 ($STT_MODEL)"
echo "  TTS (Docker):   http://tts:8880/v1 ($TTS_MODEL / $TTS_VOICE)"
echo ""
echo "Tip: To enable camera vision, run:"
echo "  OLLAMA_LLM_MODEL=llama3.2-vision ./setup-local-ai.sh"
