@documents @regression
Feature: Загрузка документов в RAG-хранилище

  Scenario: Успешная синхронная индексация
    When я отправляю POST /api/v1/documents
      с title "CVE-2021-44228" и content "Log4Shell описание..."
    Then ответ имеет статус 200
    And document_id является UUID
    And indexed равен true
    And документ доступен для поиска через /ask

  Scenario: Слишком большой документ
    When я отправляю POST /api/v1/documents с content размером 2 МБ
    Then ответ имеет статус 413

  Scenario: Пустой title отклоняется
    When я отправляю POST /api/v1/documents с пустым title
    Then ответ имеет статус 422