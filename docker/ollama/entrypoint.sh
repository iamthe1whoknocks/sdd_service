#!/bin/sh
# Entrypoint контейнера ollama: предзагрузка моделей, затем постоянный сервер.
#
# Переменные окружения:
#   OLLAMA_EMBED_MODEL - embedding model (default: nomic-embed-text:latest, dim=768)
#   OLLAMA_LLM_MODEL   - LLM для генерации ответов (default: qwen2.5:3b)
#
# Примечание: в образе ollama нет curl/wget, поэтому готовность сервера и
# наличие моделей проверяем через `ollama list` (CLI сам ходит в локальный API).

set -e

echo "[ollama-entrypoint] старт ollama serve"
ollama serve &
SERVER_PID=$!

# Аккуратно завершаем и сервер при остановке контейнера
trap 'kill $SERVER_PID 2>/dev/null || true; exit 0' TERM INT

# Ждём готовности HTTP API (обычно поднимается за секунды)
echo "[ollama-entrypoint] ожидание готовности сервера"
for i in $(seq 1 120); do
  if ollama list >/dev/null 2>&1; then
    break
  fi
  if [ "$i" = "120" ]; then
    echo "[ollama-entrypoint] сервер не поднялся за 120 секунд" >&2
    exit 1
  fi
  sleep 1
done

if [ -n "$OLLAMA_EMBED_MODEL" ]; then
  echo "[ollama-entrypoint] скачивание модели эмбеддингов: $OLLAMA_EMBED_MODEL"
  ollama pull "$OLLAMA_EMBED_MODEL"
fi

if [ -n "$OLLAMA_LLM_MODEL" ]; then
  echo "[ollama-entrypoint] скачивание LLM: $OLLAMA_LLM_MODEL"
  ollama pull "$OLLAMA_LLM_MODEL"
fi

echo "[ollama-entrypoint] модели готовы, сервер продолжает работу"
wait "$SERVER_PID"