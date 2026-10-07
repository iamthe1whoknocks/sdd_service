@ask @regression
Feature: Вопрос-ответ по уязвимостям

  Scenario: Успешный ответ с источниками
    Given в хранилище загружен документ про CVE-2021-44228
    When я отправляю POST /api/v1/ask с вопросом "что такое Log4Shell?"
    Then ответ имеет статус 200
    And в answer есть текст о Log4Shell
    And sources содержит от 1 до 5 источников
    And каждый источник имеет title и snippet

  Scenario: Пустой вопрос отклоняется
    When я отправляю POST /api/v1/ask с пустым question
    Then ответ имеет статус 422
    And в теле ошибки есть detail

  Scenario: Таймаут генерации
    Given Ollama отвечает дольше 60 секунд
    When я отправляю POST /api/v1/ask с любым вопросом
    Then ответ имеет статус 504