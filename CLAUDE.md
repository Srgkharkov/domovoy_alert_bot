# CLAUDE.md

Telegram-бот на Go для управления умным домом Aqara. Обзор — в README.md, устройство — в docs/architecture.md.

## Правила

- **Документацию обновлять вместе с кодом**, в том же изменении:
  - CHANGELOG.md → раздел `[Unreleased]`
  - новые env-переменные → `.env.example` + docs/setup.md
  - новые команды бота → README.md, `commandList()`/`helpText` в internal/bot/handlers.go
  - новые intents Aqara → таблица в docs/architecture.md
  - выполненные/новые пункты → docs/roadmap.md
- Репозиторий публичный: никаких токенов, IP сервера, личных данных в коде и доках (IP — только в `.env.deploy`).
- Перед коммитом: `make lint test`.
- Тексты для пользователей бота — на русском, комментарии в коде — на английском.
