# Деплой на VPS

Команды из Telegram бот получает через long polling. Входящие порты нужны только для **Aqara Message Push** (мгновенные алерты): Caddy принимает HTTPS на 80/443 и проксирует `/aqara/push/<secret>` в контейнер бота. Без `PUBLIC_DOMAIN` и `PUSH_SECRET` push выключен, и алерты работают только через резервный опрос.

## DNS и HTTPS

1. Создайте у регистратора A-запись для поддомена, например `domovoy.example.ru`, на IP VPS. **Подчёркивания в имени использовать нельзя**: Let's Encrypt не выдаёт на такие имена сертификаты.
2. Пропишите в `.env`: `PUBLIC_DOMAIN=domovoy.example.ru`, `PUSH_SECRET=$(openssl rand -hex 24)`.
3. Порты 80 и 443 на сервере должны быть свободны: их займёт Caddy, он же сам получит и будет продлевать сертификат. Если на сервере уже есть nginx, уберите сервис `caddy` из compose, опубликуйте порт `bot` на `127.0.0.1:8080` и проксируйте `location /aqara/push/` из nginx.
4. Проверка: `curl https://domovoy.example.ru/healthz` → `ok`.

## Настройка push в консоли Aqara

developer.aqara.com → проект → **Message push**:

- Регион: тот же, что `AQARA_API_URL` (например, Russian).
- **Get messages based on HTTP**.
- Режим подписки: **User-defined subscription mode** (бот сам подписывается на датчики при старте) или **All receiving mode (Resource)** (все изменения всех устройств, включая свет — больше трафика).
- Адрес: `https://<PUBLIC_DOMAIN>/aqara/push/<PUSH_SECRET>`, затем **Verify**.
- Справа показаны статистика доставки и неудачные сообщения за 12 часов.

## Подготовка сервера (один раз)

Ubuntu/Debian на Hetzner Cloud:

```bash
ssh root@<VPS_IP>
curl -fsSL https://get.docker.com | sh
docker compose version
```

## Деплой с локальной машины

```bash
cp .env.deploy.example .env.deploy   # указать DEPLOY_HOST=<VPS_IP>
make deploy
```

`make deploy` копирует проект через `rsync` в `~/domovoy_alert_bot` (вместе с `.env`) и выполняет там `docker compose up -d --build`.
Файл `.env.deploy` лежит в `.gitignore`, поэтому адрес сервера не попадёт в публичный репозиторий.

Первая авторизация Aqara на сервере:

```bash
ssh root@<VPS_IP>
cd ~/domovoy_alert_bot
docker compose run --rm bot auth request
docker compose run --rm bot auth exchange <code>
```

## Эксплуатация

| Задача | Команда |
|---|---|
| Логи | `make deploy-logs` |
| Перезапуск | `docker compose restart` (на сервере) |
| Обновление | `make deploy` |
| Бэкап токена | `docker run --rm -v domovoy_alert_bot_bot-data:/d alpine cat /d/aqara_token.json` |

Ротация логов настроена в `docker-compose.yml` (3 × 10 МБ).

## Безопасность

- `.env` на сервере должен быть доступен только владельцу: `chmod 600 .env`.
- Контейнер работает от пользователя `nonroot`, образ distroless (в нём нет shell).
- Войти по SSH должно быть можно только по ключу (`PasswordAuthentication no`).
