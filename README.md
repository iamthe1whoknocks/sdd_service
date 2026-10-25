# VulnRAG

RAG-ассистент по открытым источникам об уязвимостях (spec-driven проект).
Спецификация: [`docs/spec.md`](docs/spec.md), контракт API: [`api/openapi.yaml`](api/openapi.yaml).

## Состав стека (docker compose)

| Сервис | Что это | Порт на хосте |
|---|---|---|
| `web` | nginx: статика SPA + прокси `/api` → `app:8080` | 80 |
| `app` | Go API (`net/http`) | 127.0.0.1:8080 |
| `postgres` | PostgreSQL 16 + pgvector | 127.0.0.1:5432 |
| `migrate` | golang-migrate: применяет `migrations/*.up.sql` | — (однократно) |
| `ollama` | LLM + эмбеддинги | 127.0.0.1:11434 |

Цепочка запуска: `postgres (healthy)` → `migrate (completed)` → `app` (после БД, миграций и ollama) → `web`.

Модели Ollama: `paraphrase-multilingual` (эмбеддинги, dim=768 — соответствует `vector(768)` в миграции) и `qwen2.5:3b` (генерация). Настраиваются переменными `OLLAMA_EMBED_MODEL` / `OLLAMA_LLM_MODEL`.

## Быстрый старт

```bash
cp .env.example .env      # при необходимости поменять значения
make up                   # docker compose up -d --build
curl http://localhost:8080/health   # -> {"status":"ok"}
open http://localhost     # SPA
```

Полезные команды — `make help` / [`Makefile`](Makefile):

```bash
make logs                 # логи всех сервисов
make ps                   # статус контейнеров
make migrate-up           # применить миграции вручную
make migrate-down         # откатить последнюю миграцию
make ollama-models        # список моделей в ollama
make reset                # down -v: удалить тома (БД + модели!)
make test                 # unit-тесты (быстрые, без внешних сервисов)
make test-api             # интеграционные тесты против запущенного стека
```

## Первый запуск (важно)

1. Docker daemon должен быть запущен.
2. Скачиваются образы postgres/ollama/migrate (~2+ ГБ) и модели Ollama (~2,5 ГБ) —
   первый `make up` может занять 10–30 минут. `app` и `web` поднимутся только
   после готовности `postgres` и `ollama`.
3. Ограничения под 16 ГБ RAM: `shared_buffers=256MB` у postgres,
   `OLLAMA_MAX_LOADED_MODELS=1`, `OLLAMA_NUM_PARALLEL=1` у ollama.

## Разработка

- **API**: локально `go run ./cmd/server` (порт 8080). Миграции — через compose
  (`make migrate-up`) или отдельным postgres-инстансом.
- **SPA**: `cd web && npm install && npm run dev` — Vite на :5173, прокси
  `/api` и `/health` на `localhost:8080` (см. [`web/vite.config.ts`](web/vite.config.ts)).
- **Тесты**: `make test` — unit-тесты (хендлеры через `httptest` + фейковый движок,
  валидации, чанкинг); `make test-api` — интеграционные тесты против запущенного
  стека на :8080 (`go test -tags integration ./tests/`).

## Структура

```
api/                  OpenAPI-контракт (источник истины)
cmd/server/           точка входа Go API
docker/ollama/        entrypoint предзагрузки моделей
tests/                интеграционные тесты против живой API (make test-api)
migrations/           SQL-миграции (golang-migrate формат)
web/                  SPA (Vite + React + TS)