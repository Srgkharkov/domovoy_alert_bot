-include .env.deploy

BINARY      := bin/domovoy
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DEPLOY_USER ?= root
DEPLOY_PATH ?= ~/domovoy_alert_bot
SSH         := ssh $(DEPLOY_USER)@$(DEPLOY_HOST)

.PHONY: build run test lint fmt docker-up docker-logs auth-request auth-exchange deploy deploy-logs

build: ## Собрать бинарник
	go build -ldflags="-X main.version=$(VERSION)" -o $(BINARY) ./cmd/domovoy

run: ## Запустить локально с переменными из .env
	set -a && . ./.env && set +a && go run ./cmd/domovoy

test: ## Тесты
	go test -race ./...

lint: ## go vet + проверка форматирования
	go vet ./...
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

fmt:
	gofmt -w .

docker-up: ## Собрать и запустить контейнер локально
	VERSION=$(VERSION) docker compose up -d --build

docker-logs:
	docker compose logs -f

auth-request: ## Запросить код авторизации Aqara (в контейнере)
	docker compose run --rm bot auth request

auth-exchange: ## Обменять код на токен: make auth-exchange CODE=123456
	docker compose run --rm bot auth exchange $(CODE)

deploy: ## Залить на VPS и перезапустить (нужен .env.deploy)
	@test -n "$(DEPLOY_HOST)" || (echo "DEPLOY_HOST не задан, см. .env.deploy.example" && exit 1)
	$(SSH) "mkdir -p $(DEPLOY_PATH)"
	rsync -az --delete --exclude .git --exclude bin --exclude data --exclude .env.deploy ./ $(DEPLOY_USER)@$(DEPLOY_HOST):$(DEPLOY_PATH)/
	$(SSH) "cd $(DEPLOY_PATH) && VERSION=$(VERSION) docker compose up -d --build"

deploy-logs:
	$(SSH) "cd $(DEPLOY_PATH) && docker compose logs -f --tail=100"
