# Changelog

Формат: [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии по [SemVer](https://semver.org/lang/ru/).

## [Unreleased]

### Added
- Начальная структура проекта на Go.
- Клиент Aqara Open API v3: подпись запросов, авторизация по коду, автообновление токена.
- Telegram-команды `/devices`, `/scenes` (с кнопками запуска), `/chatid`, `/help`.
- Алерты в чат: протечка воды и низкий заряд батареи (пакет `internal/alert`).
- HTTP-приёмник Aqara Message Push (`/aqara/push/<secret>`) и Caddy с автоматическим HTTPS в docker-compose.
- Автоподписка датчиков на push (`config.resource.subscribe`) для режима User-defined.
- Резервный опрос датчиков протечки (`ALERT_POLL_INTERVAL`, по умолчанию 5 мин) с дедупликацией событий.
- Команда `/light` — пульт: inline-кнопки для каналов реле и розеток с текущим состоянием и подписью «кто переключил».
- CLI-команда `domovoy check` — список домов, сценариев и устройств, видимых боту.
- Повторные попытки подключения к Telegram при старте (экспоненциальная задержка).
- Ограничение доступа по белому списку ID чатов.
- Dockerfile (distroless), docker-compose, Makefile с целями для деплоя.
- GitHub Actions CI.
- Документация: setup, deployment, architecture, roadmap.

- Доступ по запросу: при `TELEGRAM_ADMIN_CHAT_ID` сообщения из незнакомых чатов превращаются в запрос администратору с кнопками «Разрешить / Отклонить»; решения хранятся в `DATA_DIR/access.json`.
- Команда `/access` для администратора: список одобренных чатов и отзыв доступа.

### Security
- Токен Telegram-бота вырезается из ошибок и логов (net/http включает его в URL запроса).
