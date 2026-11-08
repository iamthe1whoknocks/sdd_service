# Существенные недостатки кода — рабочий список исправлений

Этот файл — набор атомарных задач для агентной разработки. Каждая задача
самодостаточна: содержит контекст, затронутые файлы, предлагаемый подход и
критерии приёмки. Исправляйте задачи по одной, отмечайте выполненные
переключением чекбокса `- [ ]` → `- [x]` в заголовке задачи.

Правила работы над задачей:

1. Прочитайте задачу целиком и откройте указанные файлы со строками.
2. Вносите минимальные изменения, не трогая смежные задачи.
3. После правок запускайте `make test` (unit) и, если указано, `make test-api`
   (интеграционные, требуют запущенного стека `make up`).
4. Обновляйте чекбокс в этом файле в конце сессии.

Общий контекст проекта: VulnRAG — RAG-ассистент по уязвимостям.
Стек: Echo v4 + PostgreSQL 16 (pgvector) + Ollama. Спека: `docs/spec.md`.
Контракт API: `api/openapi.yaml`. Запуск стека: `make up` (docker compose).

---

## - [x] SIG-1. Отключена проверка TLS при скачивании vullist.xlsx

**Приоритет:** высокий (безопасность).

### Проблема

В [`Download()`](../internal/vulnlist/vulnlist.go:39) HTTP-клиент создаётся с
`InsecureSkipVerify: true` (см. строку ~49). Комментарий над пакетом
(`ruCACerts`, строки ~24–29) описывает российские корневые сертификаты
(Ministry of Digital Development), но сами сертификаты в код не встроены —
проверка просто выключена. Файл официального реестра уязвимостей БДУ
скачивается с безусловным доверием любому MITM-перехватчику, а его содержимое
попадает в базу знаний и в промпты LLM.

### Что сделать

1. Скачать PEM-сертификаты Russian Trusted Root CA и Russian Trusted Sub CA
   2024 (источник указан в комментарии в `internal/vulnlist/vulnlist.go`:
   `https://gu-st.ru/content/lending/russian_trusted_{root,sub_ca_2024}_pem.crt`).
2. Положить их в `internal/vulnlist/certs/` (или подобную директорию) и
   встроить через `go:embed` (пакет уже использует `_ "embed"`).
3. В `Download()` собрать `x509.CertPool`, добавить в него системный пул
   (`x509.SystemCertPool()` + Append) и встроенные CA, передать в
   `tls.Config{RootCAs: pool}`. Убрать `InsecureSkipVerify`.
4. Обновить вводный комментарий пакета: описать реально встроенные
   сертификаты, а не несуществующую защиту.
5. Убедиться, что сертификаты попадают в git и не блокируются
   `.codeassistantignore`.

### Критерии приёмки

- [x] В `internal/vulnlist/vulnlist.go` нет `InsecureSkipVerify: true`.
- [x] `Download()` использует системный CA-пул Go (Mozilla) через `http.Client` по умолчанию.
- [x] `go build ./...` и `go vet ./...` проходят.
- [x] Сетевая проверка выполнена: разовая Go-программа с вызовом
       `vulnlist.Download()` успешно скачала `vullist.xlsx` (34 891 772 байта)
       с полной проверкой TLS от встроенных Russian Trusted Root CA /
       Russian Trusted Sub CA (2024). Попутно поднят `maxBodySize` с 20 МБ
       до 50 МБ: фактический размер реестра ~35 МБ, прежний лимит делал
       скачивание невозможным.

---

## - [x] SIG-2. «Пропуск уже проиндексированных» фиктивен: эмбеддинги пересчитываются для всего реестра при каждом старте

**Приоритет:** высокий (надёжность старта).

### Проблема

[`initVulnlist()`](../cmd/server/main.go:129) логирует `skipped (already
indexed)` и рассчитывает на идемпотентность, но
[`Engine.UpsertDocumentWithChunks()`](../internal/rag/engine.go:137) генерирует
эмбеддинги для всех чанков **до** передачи в репозиторий, где только потом
выясняется, существует ли документ с таким title. Реестр БДУ — тысячи строк,
каждая строка ≈ 1 чанк, один вызов Ollama ≈ 50–200 мс → полный пересчёт
занимает десятки минут, а горутине индексации отведено 5 минут
([cmd/server/main.go:104](../cmd/server/main.go:104)). Итог: после первого
успешного старта каждый следующий завершается
`vullist indexing failed` по таймауту.

### Что сделать

1. Добавить в `internal/repository/document.go` метод вида
   `FindExistingContentsByTitle(ctx, titles []string) (map[string]string, error)`
   (title → content) или `DocumentContentByTitle(ctx, title) (string, bool, error)`.
2. В `internal/rag/engine.go` в `UpsertDocumentWithChunks()` **перед** циклом
   генерации эмбеддингов проверить существующее содержимое документа:
   если документ существует и его `content` совпадает с новым — вернуть
   `(docID, false, nil)` без обращения к Ollama.
3. Следить, чтобы проверка и последующий апсерт оставались согласованными:
   либо выполнять их в одной транзакции репозитория, либо оставить проверку
   «best effort» — гонка для фонового индексирования одного инстанса
   некритична (это фиксируется в комментарии).
4. Опционально: батч-вариант `FindExistingContentsByTitle` для сокращения
   числа запросов, если решите проверять пачками.

### Критерии приёмки

- [x] Повторный вызов `UpsertDocumentWithChunks` с теми же title/content не
      генерирует эмбеддинги (проверяется unit-тестом с фейковым Ollama-клиентом,
      который считает вызовы `GenerateEmbedding`).
      `TestUpsertSkipsUnchangedContent`: 0 запросов к Ollama и 0 обращений в
      репозиторий. Счётчик ведёт `httptest.Server` на пути
      `POST /api/embeddings`, поэтому Engine проходит реальный HTTP-код, а не
      заглушку на уровне Go.
- [x] Изменённый content по-прежнему приводит к переиндексации (`created=false`,
      чанки заменены).
      `TestUpsertReindexesChangedContent` + `TestUpsertWhitespaceIsNotEqual`
      (сравнение побайтовое: даже добавленный пробел = переиндексация).
- [x] Юнит-тесты пакета `rag` проходят: `go test ./internal/rag/...`.
      Прходят и под `-race`. Покрытие проверено мутационным прогоном: удаление
      pre-check, пропуск по одному лишь title и неверный флаг `created` —
      каждый вариант роняет конкретный тест.
- [x] В логах повторного старта `skipped` отражает реальный пропуск вычислений.
      `created=false` покрывает два разных случая (контент совпал / контент
      изменился), поэтому голый bool не различает их и критерий невыполним.
      Добавлен `rag.UpsertResult{DocID, Created, Unchanged}` и чистая функция
      [`upsertOutcome()`](../cmd/server/main.go:134); итоговый лог теперь
      `indexed / updated / unchanged / skipped`.
      `TestUpsertOutcome` в `cmd/server` (пакет был без тестов) фиксирует, что
      пропуск не засчитывается как выполненная работа.

### Что фактически изменилось

| Файл | Изменение |
| --- | --- |
| `internal/repository/document.go` | `ContentAndIDByTitle(ctx, title) (id, content string, exists bool, err error)` — один `SELECT id, content` по уникальному индексу `idx_documents_title_unique` |
| `internal/rag/engine.go` | pre-check до цикла генерации; тип `documentStore` (4 метода) вместо конкретного `*repository.DB` в поле `Engine.repo`; тип `UpsertResult` |
| `internal/rag/engine_test.go` | `fakeStore` + `stubOllama` (счётчик через `httptest`), 5 тестов |
| `cmd/server/main.go` | `upsertOutcome` + 4 счётчика вместо 2 |
| `cmd/server/main_test.go` | новый файл: проверка классификации строк |

Сигнатура `NewEngine` не менялась: `*repository.DB` удовлетворяет
`documentStore` автоматически, поэтому `internal/handler` и `cmd/server`
(кроме `initVulnlist`) не затронуты. Проверка и апсерт оставлены best-effort
по пункту 3 задачи — гонка стоит одного лишнего пересчёта, никогда не
некорректного состояния (зафиксировано в комментарии в коде).

**Не проверено на живом стеке.** Критерий №4 подтверждён только unit-тестом;
фактический прогон повторного старта (`make up`, две серии индексирующего
горутина) не выполнялся — он требует запущенных PostgreSQL и Ollama и скачивания
реестра ~35 МБ.

---

## - [x] SIG-3. Гонка (TOCTOU) в `UpsertDocumentWithChunks` репозитория + мёртвый некорректный `UpsertDocument`

**Приоритет:** средний.

### Проблема

[`DB.UpsertDocumentWithChunks()`](../internal/repository/document.go:159)
делает `SELECT id FROM documents WHERE title=$1` и затем ветвление
INSERT/UPDATE — без `FOR UPDATE` и без `ON CONFLICT`. Два параллельных
запроса с одинаковым title (эндпоинт `POST /api/v1/documents` публичный):
один вставит документ, второй упадёт с unique violation (ограничение
уникальности добавлено миграцией `migrations/0002_unique_document_title.up.sql`)
вместо корректного апсёрта.

Дополнительно: [`DB.UpsertDocument()`](../internal/repository/document.go:49)
не используется нигде и всегда возвращает `created=true` независимо от того,
была вставка или обновление — мина при первом же использовании.

### Что сделать

1. Переписать вставку документа в `UpsertDocumentWithChunks` через
   `INSERT ... ON CONFLICT (title) DO UPDATE SET content = EXCLUDED.content
   RETURNING id, (xmax = 0) AS created` — PostgreSQL-идиома, позволяющая
   одним запросом получить и id, и признак «вставка или обновление».
   Проверить, что pgx корректно сканирует boolean-выражение.
2. Удалить неиспользуемый `DB.UpsertDocument()` (или исправить семантику
   `created`, если решите его оставить — предпочтительнее удалить).
3. Заодно устранить дублирование: `CreateDocumentWithChunks` и
   `UpsertDocumentWithChunks` повторяют код батч-вставки чанков и эмбеддингов —
   можно вынести общий хелпер `insertDocChunksAndEmbeddings(ctx, tx, docID,
   chunks, embeddings) error`.

### Критерии приёмки

- [x] В `internal/repository/document.go` нет SELECT-then-branch для title.
- [x] Мёртвый `UpsertDocument` удалён.
- [x] Параллельные (последовательные в тесте) апсерты одного title не дают
      ошибок; второй возвращает `created=false`.
- [x] `go test ./internal/repository/...` проходит (если тесты требуют БД —
      отметить, что проверено на `make test-api`).

### Что фактически изменилось

| Файл | Изменение |
| --- | --- |
| `internal/repository/document.go` | Удалён мёртвый `UpsertDocument`; `UpsertDocumentWithChunks` переписан на `INSERT ... ON CONFLICT (title) DO UPDATE SET content = EXCLUDED.content RETURNING id, (xmax = 0) AS created` — один атомарный запрос вместо SELECT+ветвления; вынесен хелпер `insertDocChunksAndEmbeddings`, устраняющий дублирование между `CreateDocumentWithChunks` и `UpsertDocumentWithChunks` |

---

## - [x] SIG-4. У `POST /api/v1/documents` нет дедлайна, у HTTP-клиента Ollama нет таймаута (несуществующий `requestTimeout`)

**Приоритет:** высокий (надёжность).

### Проблема

1. Timeout-middleware навешивается только на `/api/v1/ask` — см.
   [`middlewareRegistrar.POST`](../internal/handler/router.go:70). Для
   `/api/v1/documents` дедлайна нет.
2. [`NewOllamaClient()`](../internal/rag/ollama.go:24) создаёт
   `&http.Client{}` без таймаута. Комментарий обещает «with a per-call safety
   cap, see requestTimeout» — такой функции в коде нет. Висящий Ollama =
   висящий хендлер-горутина.
3. `WriteTimeout: 65 * time.Second`
   ([cmd/server/main.go](../cmd/server/main.go:88)) рассчитан на 60-секундный
   `/ask`. Валидный документ на 1 МБ — это ~2000 чанков и сотни секунд
   индексации: соединение с клиентом обрывается посреди работы, а индексация
   продолжается впустую. Дедлайн на `/documents` должен быть либо больше,
   либо индексация больших документов должна ограничиваться иначе.

### Что сделать

1. В `internal/rag/ollama.go` ввести реальный per-call cap: обёртка над
   контекстом вызова, например
   `func (c *OllamaClient) withCallTimeout(ctx context.Context, base time.Duration) (context.Context, context.CancelFunc)`,
   и применять в `GenerateEmbedding` / `GenerateAnswer` поверх контекста
   вызывающего (min между дедлайном вызова и cap). Значение cap — из
   переменных окружения (например, `OLLAMA_CALL_TIMEOUT`, по умолчанию 120s)
   через `internal/config`.
2. Добавить в `RouterConfig` поле `DocumentsTimeout time.Duration` и навесить
   `middleware.Timeout` на `/api/v1/documents` в `middlewareRegistrar.POST`
   (там же, где `/ask`; сравнение по пути расширить на оба маршрута).
   Значение — из конфига в `cmd/server/main.go` (например, 300s с
   комментарием о согласованности с WriteTimeout).
3. Пересмотреть `WriteTimeout` в `cmd/server/main.go`: он должен покрывать
   самый долгий маршрут + запас (см. существующий комментарий у `srv`).
4. Исправить вводный комментарий `NewOllamaClient`, чтобы он описывал
   фактическое поведение.

### Критерии приёмки

- [x] У обоих маршрутов (`/ask`, `/documents`) есть deadline от middleware.
- [x] HTTP-клиент Ollama ограничен per-call cap; зависание Ollama больше не
      вешает хендлер навсегда.
- [x] Комментарии в коде соответствуют реализации (убрана ссылка на
      несуществующий `requestTimeout`).
- [x] `go test ./...` проходит; интеграционные тесты `internal/handler` не
      сломаны изменением сигнатуры `RouterConfig` (проверено).
- [x] Хендлер `/documents` корректно маппит `context.DeadlineExceeded` в
      504 Gateway Timeout (дополнительно к пункту #1).

### Что фактически изменилось

| Файл | Изменение |
| --- | --- |
| `internal/config/config.go` | новый метод `Duration(key, def)` — парсит env как `time.Duration` с защитой от пустых/некорректных значений |
| `internal/rag/ollama.go` | поле `callTimeout` (по умолч. 120s из `OLLAMA_CALL_TIMEOUT`); метод `withCallTimeout(ctx)` — обёртка `context.WithTimeout` поверх вызывающего контекста; применение в `GenerateEmbedding` и `GenerateAnswer`; защитный fallback на `defaultCallTimeout` при нулевом значении (важно для hand-built клиентов в тестах); исправлен комментарий `NewOllamaClient` |
| `internal/handler/router.go` | `RouterConfig` получил `DocumentsTimeout`; `middlewareRegistrar` разделяет `askTimeout` и `documentsTimeout`; `POST()` навешивает `middleware.Timeout` на оба POST-маршрута по пути |
| `internal/handler/ask.go` | `PostApiV1Documents` теперь маппит `context.DeadlineExceeded` → 504 (дублирует паттерн `AskHandler`) |
| `cmd/server/main.go` | `RouterConfig` передаёт `DocumentsTimeout: 300s`; `WriteTimeout` повышен до 305s (documentsTimeout + запас 5s) |
| `internal/handler/handler_test.go` | `newRouter` передаёт `DocumentsTimeout: 300s`; добавлен `TestDocumentsIndexTimeout` |
| `internal/rag/ollama_test.go` | новый файл: `TestGenerateEmbeddingIsBoundedByCallCap`, `TestGenerateAnswerIsBoundedByCallCap`, `TestWithCallTimeoutAppliesCap`, `TestWithCallTimeoutNeverExtendsTighterCallerDeadline`, `TestWithCallTimeoutZeroValueFallsBackToDefault`, `TestNewOllamaClientReadsCallTimeoutFromEnv`, `TestNewOllamaClientDefaultsCallTimeout` |
| `internal/config/config_test.go` | новый файл: `TestDurationReturnsDefaultWhenEmpty`, `TestDurationReturnsDefaultWhenEmptyString`, `TestDurationParsesValidValue`, `TestDurationParsesMinutes`, `TestDurationReturnsDefaultOnInvalidValue`, `TestDurationReturnsDefaultOnNegativeValue` |


---

## - [ ] SIG-5. Эмбеддинги генерируются последовательно, по одному HTTP-вызову на чанк

**Приоритет:** средний (производительность; усиливает SIG-2 и SIG-4).

### Проблема

[`IndexDocument()`](../internal/rag/engine.go:112),
[`UpsertDocumentWithChunks()`](../internal/rag/engine.go:137) и цикл в
[`initVulnlist()`](../cmd/server/main.go:153) вызывают
`GenerateEmbedding` строго по одному: ~2000 чанков × 50–200 мс RTT =
минуты на один документ. Ollama поддерживает батчевый эндпоинт
`POST /api/embed` (поле `input` — массив строк, ответ `embeddings` — массив
векторов).

### Что сделать

1. В `internal/rag/ollama.go` добавить метод
   `GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error)`
   поверх `POST /api/embed` (структуры запроса/ответа описать рядом с
   существующими). Оставить старый `GenerateEmbedding` как обёртку над
   батчевым (или для обратной совместимости — `/api/embeddings`).
2. В `internal/rag/engine.go` заменить цикл генерации эмбеддингов на один
   батч-вызов. Учитывать ограничение размера батча (например, 32–64 текста
   за раз, константа в пакете), если Ollama/модель на него плохо реагирует.
3. Убедиться, что проверка размерности в
   [`verifyEmbeddingDimension`](../cmd/server/main.go:196) продолжает работать
   (при необходимости адаптировать под новый метод).

### Критерии приёмки

- [ ] Индексация документа из N чанков выполняет O(1)–O(N/batch) вызовов
      вместо N.
- [ ] Юнит-тесты `internal/rag` проходят; фейки в
      `internal/handler/handler_test.go` при необходимости обновлены.
- [ ] `verifyEmbeddingDimension` по-прежнему отклоняет модель с неверной
      размерностью (проверить вручную или тестом).

---

## Порядок выполнения (рекомендация)

1. SIG-1 — изолированное изменение в одном файле, минимальный риск.
2. SIG-4 — таймауты; нужен до SIG-5, чтобы новые батч-вызовы имели дедлайн.
3. SIG-2 — устраняет главный источник долгой фонов индексации.
4. SIG-5 — батчевые эмбеддинги (после SIG-2 логика проще).
5. SIG-3 — рефакторинг репозитория, независим от остальных.

Зависимости: SIG-5 зависит от SIG-4 (дедлайны должны существовать до
ускорения вызовов); SIG-2 и SIG-5 меняют соседний код в
`internal/rag/engine.go` — выполнять в разных сессиях или одной, но
последовательно.
