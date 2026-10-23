# FIXES — чеклист исправлений по итогам code review

> Дата ревью: 2026-10-19. Спецификация: [`docs/spec.md`](spec.md).
> Формат: единый чеклист для работы «один фикс = один коммит = одна точка отката».
> Статусы: `[ ]` — не начато, `[-]` — в работе, `[x]` — готово (тесты зелёные, закоммичено).

## Git-процесс (обязательно)

1. Зафиксировать baseline (состояние после ревью):
   ```bash
   git add -A && git commit -m "baseline: состояние после code review" && git tag review-baseline
   ```
2. Каждый фикс — отдельный коммит с сообщением `fix(<категория>): <что сделано>`.
3. После каждого фикса: `go test ./...` (и при желании `make test-api`) — только зелёные тесты → commit.
4. Откат на состояние после ревью: `git reset --hard review-baseline`.
5. Откат последнего фикса: `git revert HEAD` (сохраняет историю) или `git reset --hard HEAD~1`.
6. Рекомендуется работать в отдельной ветке: `git checkout -b fix/review`.

---

## 🔴 Критично

- [ ] **#1 Убрать `InsecureSkipVerify: true`** при скачивании vullist
  - Файл: `internal/vulnlist/vulnlist.go` (Transport в `Download`)
  - Критерий: TLS-верификация включена; `go vet`/`gosec` не ругается на `InsecureSkipVerify`.

- [x] **#2 Не отдавать `err.Error()` клиенту** (утечка внутренней информации)
  - Файлы: `internal/handler/ask.go` (500 в `PostApiV1Ask`, ошибки в `PostApiV1Documents`), `cmd/server/main.go` (fallback `HTTPErrorHandler`)
  - Критерий: в `detail` ответа нет внутренних URL/SQL/инфраструктуры; полный стек — только в лог.

- [x] **#3 Внутренняя ошибка индексации → 500, а не 422**
  - Файлы: `internal/handler/ask.go` (respondError 422 → 500), `internal/handler/handler_test.go` (`TestDocumentsIndexError`)
  - Критерий: падение Ollama/БД при `/documents` возвращает 500; 422 остаётся только для невалидного payload; тест обновлён.

- [x] **#4 Реальный таймаут 60 с для `/ask`** вместо строковой проверки
  - Файлы: `cmd/server/main.go` (`middleware.Timeout(60*time.Second)`), `internal/handler/ask.go` (`errors.Is(err, context.DeadlineExceeded)`), `internal/rag/ollama.go` (safety cap), `internal/middleware/timeout.go`
  - Критерий: превышение 60 с → 504; детект через `errors.Is(err, context.DeadlineExceeded)`/`ctx.Err()`, без сравнения строк; `WriteTimeout` согласован с 60 с.

- [x] **#5 Ограничить размер тела запроса** (защита от DoS)
  - Файлы: `cmd/server/main.go` (middleware.BodyLimit("1MB+64KB"))
  - Критерий: запрос > ~1 МБ отклоняется до полного чтения в память; возвращается 413.

- [x] **#6 Синхронизировать модель эмбеддингов и размерность `vector(768)`**
  - Файлы: `internal/rag/ollama.go` (дефолт `EMBED_MODEL`), `migrations/0001_init.up.sql` (размерность), `docker-compose.yml` (`OLLAMA_EMBED_MODEL`), `docs/spec.md` §7/§9
  - Критерий: дефолтная модель отдаёт 768 измерений (или размерность в БД приведена к модели); на старте проверяется доступность модели и совпадение размерности.

- [x] **#7 Идемпотентная/фоновая индексация vulnist**
  - Файлы: `cmd/server/main.go` (`initVulnlist`), `internal/vulnlist/vulnlist.go`, `internal/repository/document.go` (`UpsertDocumentWithChunks`), `internal/rag/engine.go`, `migrations/0002_unique_document_title`
  - Критерий: повторный старт не создаёт дубликаты документов (уникальный индекс на `documents.title`, `UpsertDocumentWithChunks`); индексация не блокирует `ListenAndServe` (фоновая горутина); есть таймаут контекста (5 мин).

## 🟠 Существенно

- [x] **#8 Валидация длин по символам (рунам), а не по байтам**
  - Файлы: `internal/handler/ask.go`, `internal/repository/embedding.go` (`len()` → `utf8.RuneCountInString`), `web/src/App.tsx` (единое правило счёта)
  - Критерий: кириллический вопрос из 2000 символов проходит; клиент и сервер дают одинаковый вердикт.
  - Решение: `question`/`title` — по рунам (UTF-8 code points); `content` — по байтам (совпадает с `octet_length` в SQL и `BodyLimit` в Echo). `api/openapi.yaml`: `maxLength` для `content` убран (байтовый лимит выражается через 413).

- [ ] **#9 Защита от prompt injection + порог релевантности источников**
  - Файлы: `internal/rag/engine.go` (пометка контекста как ненадёжного, разделители), `internal/repository/document.go` (`SearchSimilar` — порог `Score`)
  - Критерий: контекст явно помечен как внешние данные; чанки ниже порога не попадают в промпт; при пустом результате — ответ «нет релевантных данных».

- [ ] **#10 Логировать реальный HTTP-статус** (сейчас ошибки пишутся как 200)
  - Файлы: `internal/middleware/logger.go` (оценивать статус по возвращённой ошибке `*echo.HTTPError`/`c.Response().Committed`)
  - Критерий: в логе для 422/413/500/504 виден реальный статус, а не 200.

- [ ] **#11 `healthcheck` для сервиса `app`** + не публиковать лишние порты
  - Файлы: `docker-compose.yml` (healthcheck на `:8080/health`; убрать/ограничить публикацию `5432`, `11434`)
  - Критерий: `docker compose ps` показывает `app` healthy; `web` корректно стартует после `app`.

- [ ] **#12 Ограничить размер скачиваемого файла** (`io.ReadAll` без лимита)
  - Файлы: `internal/vulnlist/vulnlist.go` (`io.LimitReader`/`http.MaxBytesReader`, проверка `Content-Length`)
  - Критерий: файл больше лимита отклоняется без чтения в память целиком.

## 🟡 Средне / на улучшение

- [ ] **#13 Единый источник правил валидации** (strict-генерация oapi-codegen / TS-типы из `openapi.yaml`)
  - Файлы: `api/openapi.yaml`, `internal/handler/*`, `web/src/App.tsx`
  - Критерий: `minLength/maxLength` применяются из схемы; TS-типы генерируются, а не дублируются вручную.

- [ ] **#14 Устранить непоследовательную валидацию** (вручную vs `repository.Validate*`; `ValidateQuestion` не используется)
  - Файлы: `internal/handler/ask.go`, `internal/repository/embedding.go`
  - Критерий: один путь валидации на хендлер; валидация вынесена из `repository` в доменный слой.

- [ ] **#15 Удалить мёртвый код**
  - Файлы: `internal/middleware/logger.go` (`zapAdapter` не используется), `internal/middleware/error.go` (заглушка), `internal/repository/document.go` (поле `Answer`), дубль `getEnvDefault` (`main.go`/`ollama.go`)
  - Критерий: `go vet ./...` чисто; нет неиспользуемых сущностей.

- [ ] **#16 Батчить вставку эмбеддингов** (сейчас по одному `Exec`)
  - Файлы: `internal/repository/document.go` (`CreateDocumentWithChunks`)
  - Критерий: эмбеддинги вставляются батчем/`unnest` в одной транзакции.

- [ ] **#17 Принимать входящий `X-Request-ID`** (сейчас всегда генерируется новый)
  - Файлы: `internal/middleware/requestid.go`
  - Критерий: переданный клиентом `X-Request-ID` пробрасывается (с валидацией длины), иначе генерируется.

- [ ] **#18 UI: `AbortController` для отмены запроса, `key` источников, единый счётчик длины**
  - Файлы: `web/src/App.tsx`
  - Критерий: fetch отменяется при размонтировании/повторном запросе; счётчики совпадают с серверными правилами.

## ✅ Уже хорошо (не трогаем)

- Атомарная запись документа (эмбеддинги → одна транзакция).
- Единый контракт ошибок `{error:{type,detail}}` через `internal/httperr`.
- Unit-тесты хендлеров на фейковом движке.
- Graceful shutdown с таймаутом.
- Версионированная спека и docker-цепочка с healthcheck у postgres/migrate/ollama.

---

## Как откатиться

| Ситуация | Команда |
|---|---|
| Вернуться к состоянию после ревью | `git reset --hard review-baseline` |
| Откатить последний фикс (сохранить историю) | `git revert HEAD` |
| Откатить последний фикс (стереть) | `git reset --hard HEAD~1` |
| Откатить один файл к baseline | `git checkout review-baseline -- <file>` |
| Посмотреть, что изменилось с ревью | `git diff review-baseline` |
