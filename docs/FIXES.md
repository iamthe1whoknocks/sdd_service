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

- [x] **#9 Защита от prompt injection + порог релевантности источников**
  - Файлы: `internal/rag/engine.go` (sanitizeUntrusted, renderPrompt, promptTmpl, envMinScore, NoRelevantDataAnswer), `internal/repository/document.go` (`SearchSimilar` — порог `Score`), `docker-compose.yml` (`RAG_MIN_SCORE`), `internal/rag/engine_test.go`
  - Критерий: контекст явно помечен как внешние данные; чанки ниже порога не попадают в промпт; при пустом результате — ответ «нет релевантных данных».
  - Решение: `SearchSimilar` отфильтровывает чанки по `minScore` (cosine similarity ≥ 0.70 по умолчанию, настраивается через `RAG_MIN_SCORE`). Контекст собирается через `text/template` с разделителями `<context>`/`<source_document>`, которые экранируются в данных (`sanitizeUntrusted`). Данные и вопросы очищаются от control-символов и шаблонных слотов. При отсутствии релевантных чанков возвращается `NoRelevantDataAnswer` без вызова LLM.

- [x] **#10 Логировать реальный HTTP-статус** (сейчас ошибки пишутся как 200)
  - Файлы: `internal/middleware/logger.go` (оценивать статус по возвращённой ошибке `*echo.HTTPError`/`c.Response().Committed`)
  - Критерий: в логе для 422/413/500/504 виден реальный статус, а не 200.
  - Решение: `Logger` извлекает статус из `*echo.HTTPError` при наличии ошибки; fallback — `c.Response().Status` если ответ уже закоммичен. Echo откладывает установку `Response.Status` до момента записи в HTTP, поэтому чтение после `next(c)` давало 200 по умолчанию.

- [x] **#11 `healthcheck` для сервиса `app`** + не публиковать лишние порты
  - Файлы: `docker-compose.yml` (healthcheck на `:8080/health`; убрать/ограничить публикацию `5432`, `11434`)
  - Критерий: `docker compose ps` показывает `app` healthy; `web` корректно стартует после `app`.
  - Решение: для `app` добавлен `healthcheck` в compose (`curl -fsS http://localhost:8080/health`, `interval=5s`, `timeout=3s`, `retries=5`, `start_period=10s`); `HEALTHCHECK` из `Dockerfile` удалён, чтобы параметры правились без пересборки образа и не было двух источников. `web` уже имел `depends_on: app: condition: service_healthy` — теперь условие реально выполняется. Публикация `postgres:5432`, `ollama:11434` и `app:8080` привязана к `127.0.0.1` (было `0.0.0.0`) — порты доступны для локальной отладки (`psql`, `ollama` CLI, `make test-api`), но не торчат наружу; nginx ходит в `app:8080` по внутренней сети compose, поэтому прокси `/api` не затрагивается. Наружу опубликован только `web`. Обновлены `.env.example` и таблица портов в `README.md`, комментарии в затронутых файлах — на английском.

- [x] **#12 Ограничить размер скачиваемого файла** (`io.ReadAll` без лимита)
  - Файлы: `internal/vulnlist/vulnlist.go` (`io.LimitReader`/`http.MaxBytesReader`, проверка `Content-Length`)
  - Критерий: файл больше лимита отклоняется без чтения в память целиком.
  - Решение: `http.MaxBytesReader` ограничивает тело ответа лимитом 20 МБ (`maxBodySize`); при превышении возвращается ошибка, файл не читается в память.

## 🟡 Средне / на 

- [ ] **#13 Единый источник правил валидации** (strict-генерация oapi-codegen / TS-типы из `openapi.yaml`)
  - Файлы: `api/openapi.yaml`, `internal/handler/*`, `web/src/App.tsx`
  - Критерий: `minLength/maxLength` применяются из схемы; TS-типы генерируются, а не дублируются вручную.

- [x] **#14 Устранить непоследовательную валидацию** (правила вынесены в `internal/validate`)
  - Файлы: `internal/handler/ask.go`, `internal/validate/validate.go`, `internal/handler/errors.go`
  - Критерий: один путь валидации на хендлер; валидация вынесена из `repository` в пакет `validate`.
  - Решение: функции `Question`, `Title`, `Content` перемещены в `internal/validate` — это единый источник доменных правил лимитов (runes для title/question, bytes для content). Оба хендлера вызывают эти функции, а не дублируют проверки inline. `repository.ValidateQuestion`, `repository.ValidateTitle`, `repository.ValidateContentSize` удалены. `respondValidationError` в `errors.go` мапит `validate.Error.TooLarge` → 413, всё остальное → 422.

- [x] **#15 Удалить мёртвый код**
  - Файлы: `internal/middleware/logger.go` (`zapAdapter` удалён), `internal/middleware/error.go` (файл удалён), `internal/repository/document.go` (поле `Answer` удалено), `internal/rag/ollama.go`/`cmd/server/main.go` (дубль `getEnvDefault` удалён, единый `internal/config.String`)
  - Критерий: `go vet ./...` чисто; нет неиспользуемых сущностей.
  - Решение: `zapAdapter` (реализация `echo.Logger`, делегирующая zap) — не используется, удалён. `middleware/error.go` — заглушка с пакетным комментарием, удалён (перенесён в `logger.go`). `SearchResult.Answer` — поле не используется никем, удалено. `getEnvDefault` — дубликат в `main.go` и `rag/ollama.go`; объединён в `internal/config.String` (вызывается из `main.go`, `rag/ollama.go`, `rag/engine.go`).


- [x] **#16 Батчить вставку эмбеддингов** (сейчас по одному `Exec`)
  - Файлы: `internal/repository/document.go` (`CreateDocumentWithChunks`, `UpsertDocumentWithChunks`, хелперы `insertEmbeddingsBatch`/`buildEmbeddingInsert`), `internal/repository/document_test.go`
  - Критерий: эмбеддинги вставляются батчем/`unnest` в одной транзакции.
  - Решение: цикл из N `tx.Exec` заменён одним `INSERT INTO embeddings (chunk_id, embedding) VALUES ($1,$2),($3,$4),…` на 256 строк в запросе — лимит страхует от 65535-параметрового ограничения extended-протокола (документ до 1 МБ даёт сотни чанков). Хелпер `insertEmbeddingsBatch` общий для обоих методов, построение SQL вынесено в чистую функцию `buildEmbeddingInsert` и покрыто unit-тестами на фейковом `pgx.Tx` (живая БД не нужна). Транзакционность сохранена: все запросы идут в том же `tx`. Тип параметра не менялся — форма `VALUES ($n,$n+1)` та же, что и в построчном варианте, поэтому кодирование `[]float32 → vector` через pgx остаётся прежним. Вставка чанков оставлена на `pgx.Batch`: ей нужен `RETURNING id`, и она уже отправляется одним round-trip.

- [x] **#17 Принимать входящий `X-Request-ID`** (сейчас всегда генерируется новый)
  - Файлы: `internal/middleware/requestid.go`
  - Критерий: переданный клиентом `X-Request-ID` пробрасывается (с валидацией длины), иначе генерируется.
  - Решение: `RequestID()` читает входящий заголовок и пробрасывает его в ответный заголовок и в `c.Set("requestID", …)`, если значение проходит проверку; иначе генерирует UUID (прежнее поведение как fallback). Валидация: `TrimSpace`, лимит 128 байт (`maxRequestIDLen`), allowlist `[A-Za-z0-9._:-]` — покрывает UUID/ULID/snowflake/W3C trace-id и отсекает управляющие символы (log forging), пробелы и разметку, поскольку значение попадает в zap-лог и отражается в заголовке ответа. Невалидный заголовок не отклоняет запрос (400), а тихо заменяется сгенерированным ID. Заголовок ставится до `next(c)`, поэтому он присутствует и в ошибках 422/413/504. В `cmd/server/main.go` `RequestID()` поднят первым в цепочке `e.Use`, чтобы ID был доступен всем последующим middleware, включая отклонённые `BodyLimit` и 404. Покрыто таблицей тестов в `internal/middleware/middleware_test.go` (проброс валидного, обрезка пробелов, граница 128/129, CRLF/разметка/non-ASCII → генерация, уникальность, наличие заголовка на ошибке).

- [x] **#18 UI: `AbortController` для отмены запроса, `key` источников, единый счётчик длины**
  - Файлы: `web/src/App.tsx`, `web/src/validation.ts` (новый)
  - Критерий: fetch отменяется при размонтировании/повторном запросе; счётчики совпадают с серверными правилами.
  - Решение: три части, все в пределах фронтенда (контракт API не менялся).
    1. **Отмена запросов.** `postJSON` принимает необязательный `AbortSignal` и передаёт его в `fetch`. В `AskTab` и `DocumentsTab` появился `abortRef`: перед каждым submit отменяется предыдущий незавершённый запрос, а `useEffect(() => () => abortRef.current?.abort(), [])` отменяет активный запрос при размонтировании. Это закрывает основной сценарий — переключение вкладок уничтожает компонент, тогда как `/ask` может идти до 60 с (spec §4) и продолжал занимать генерацию в Ollama с последующим `setState` на мёртвом компоненте. `AbortError` отсекается в `catch` (`isAbortError`): отмена не показывается как ошибка API и не затирает предыдущий успешный ответ.
    2. **Единый источник правил длины.** Правила вынесены в `web/src/validation.ts` — зеркало `internal/validate` (см. #14): `question`/`title` считаются по code points (`Array.from(...).length`, как `utf8.RuneCountInString`), `content` — по байтам (`TextEncoder`, как `octet_length` в SQL и `BodyLimit` в Echo), все проверки длины идут по trimmed-значению, потому что хендлеры обрезают пробелы до валидации. Из формы убран `maxLength`: HTML-атрибут считает UTF-16 code units (MDN), поэтому 500 эмодзи-символов браузер обрезал бы примерно на 250, когда сервер принимает 500 рун — клиент был строже сервера. Проверка паритета прогнана таблицей из 25 кейсов (кириллица, эмодзи, NBSP, trailing пробелы, границы 1 МБ и 4000/500 рун): вердикты `validation.ts` и `internal/validate` совпали во всех. Inline-цепочки тернарников из компонентов убраны, вызывается `validateQuestion`/`validateTitle`/`validateContent`. Полная генерация TS-типов и правил из `openapi.yaml` остаётся в #13.
    3. **Стабильные `key` источников.** `key={i}` заменён на композитный ключ из `title` + `url` + префикса `snippet` (64 символа) со счётчиком повторов (`buildSourceKeys`): несколько чанков одного документа дают одинаковые `title`/`snippet`, поэтому без счётчика были бы дубликаты ключей. Идеальное решение — отдать наружу `chunk_id` и сделать `key` по нему, но это меняет схему `Source` в `openapi.yaml` и спеке, поэтому вынесено отдельно (см. #20).

- [x] **#19 Сгенерированный роутер (`handler.RegisterHandlers`) используется и в проде, и в тестах**
  - Файлы: `internal/handler/router.go` (новый), `cmd/server/main.go`, `internal/handler/handler_test.go`
  - Критерий: в проде и в тестах один и тот же `ServerInterface` оборачивается через `RegisterHandlers`, а не вручную.
  - Решение: создан `internal/handler/router.go` — единая функция `NewRouter`, вызываемая из `main.go` (prod) и `handler_test.go` (tests). Она создаёт Echo, настраивает HTTPErrorHandler, глобальные middleware (RequestID, Logger, Recover, BodyLimit) и регистрирует маршруты через сгенерированный `RegisterHandlers` с `middlewareRegistrar`, который добавляет `Timeout(60s)` только на `POST /api/v1/ask`. Старая ручная регистрация (`e.POST(...)`) и inline `/health` удалены из `main.go`. Тестовый `fakeEngine`/`api`/`newRouter` заменены на общий `handler.Server` + `handler.NewRouter`. Формат `BodyLimit` исправлен с `"1MB+64KB"` (не поддерживается echo v4.13.4) на `"1MB"`.


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
