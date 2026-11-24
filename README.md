# Domovoy Alert Bot 🏠

Telegram-бот для управления умным домом **Aqara** из семейного чата.
Все участники разрешённого чата могут смотреть статус устройств и запускать сценарии.

> «Домовой» — дух-хранитель дома в славянской мифологии.

[![CI](../../actions/workflows/ci.yml/badge.svg)](../../actions/workflows/ci.yml)

## Возможности

- **Алерты в чат**: протечка воды (мгновенно через Aqara Message Push, с резервным опросом), низкий заряд батареи
- `/light` — пульт освещения: кнопки включения и выключения реле и розеток, панель обновляется на месте
- **Несколько домов**: Aqara и Tuya (Tuya Smart / Smart Life) в одном боте, у каждого дома свои чаты
- `/status` — показания датчиков: температура, влажность, заряд, мощность и счётчик электроэнергии (Tuya)
- `/devices` — список устройств дома и их online/offline-статус
- `/scenes` — сценарии Aqara Home с кнопками запуска
- `/chatid` — ID текущего чата (для настройки доступа)
- Доступ только из чатов в белом списке (`TELEGRAM_ALLOWED_CHAT_IDS`)
- Доступ по запросу: новый пользователь пишет боту, администратору приходит запрос с кнопками «✅ <дом> / ❌»; `/access` — список и отзыв доступа
- Автоматическое обновление токена Aqara
- Запуск в Docker: distroless-образ размером несколько МБ, процесс не от root

Планы — в [docs/roadmap.md](docs/roadmap.md).

## Стек

| | |
|---|---|
| Язык | Go 1.27, только стандартная библиотека и [telegram-bot-api](https://github.com/go-telegram-bot-api/telegram-bot-api) |
| Умный дом | [Aqara Open API v3](https://developer.aqara.com) |
| Деплой | Docker Compose на VPS (Hetzner), Caddy (автоматический HTTPS) |
| CI | GitHub Actions: vet, gofmt, тесты, сборка образа |

## Быстрый старт

```bash
cp .env.example .env          # заполнить ключи Telegram и Aqara
make docker-up                # собрать и запустить
make auth-request             # Aqara пришлёт код на AQARA_ACCOUNT
make auth-exchange CODE=XXXX  # сохранить токен
```

Подробно: [docs/setup.md](docs/setup.md) · деплой на VPS: [docs/deployment.md](docs/deployment.md)

## Структура проекта

```
cmd/domovoy/         точка входа и CLI (run / auth / version)
internal/config/     загрузка настроек из переменных окружения
internal/aqara/      клиент Aqara Open API: подпись запросов, токены, устройства, сценарии
internal/bot/        Telegram-бот: команды, кнопки, проверка доступа
internal/alert/      алерты: правила, дедупликация push + опроса
docs/                документация
```

Как это устроено: [docs/architecture.md](docs/architecture.md).

## Разработка

```bash
make test   # тесты
make lint   # go vet + gofmt
make run    # локальный запуск с .env
```

История изменений: [CHANGELOG.md](CHANGELOG.md).

## Лицензия

[MIT](LICENSE)
