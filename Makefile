# VulnRAG — вспомогательные команды для работы со стеком.
# Перед первым запуском: cp .env.example .env (при необходимости).

COMPOSE ?= docker compose
# MIGRATE_URL передаётся в контейнер migrate через переменную DATABASE_URL,
# которую сервис migrate получает из compose (см. docker-compose.yml).

.PHONY: up down build logs ps status reset migrate-up migrate-down ollama-models run test test-features godog

## up — собрать и запустить весь стек в фоне
up:
	$(COMPOSE) up -d --build

## down — остановить стек (данные сохраняются)
down:
	$(COMPOSE) down

## build — пересобрать образы без запуска
build:
	$(COMPOSE) build

## logs — логи всех сервисов (Ctrl+C для выхода)
logs:
	$(COMPOSE) logs -f

## ps — статус контейнеров
ps:
	$(COMPOSE) ps

## status — статус контейнеров и healthcheck-цепочка
status: ps

## reset — остановить стек и удалить тома (postgres + модели ollama!)
reset:
	$(COMPOSE) down -v

## migrate-up — применить миграции вручную
migrate-up:
	$(COMPOSE) run --rm migrate

## migrate-down — откатить последнюю миграцию
migrate-down:
	$(COMPOSE) run --rm --entrypoint sh migrate -c 'migrate -path=/migrations -database="$$DATABASE_URL" down 1'

## ollama-models — список моделей в контейнере ollama
ollama-models:
	$(COMPOSE) exec ollama ollama list

## run — запустить Go-сервер локально
run:
	go run ./cmd/server/

## test — запустить godog контрактные тесты
test:
	go test -v ./tests/ -args --format=pretty

## test-features — запустить godog с параметрами из features/
test-features:
	go run ./tests/ --format=pretty --paths=features/
