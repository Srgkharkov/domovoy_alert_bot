# Архитектура

```
 Семейный чат ──► Telegram Bot API ◄── long polling ── ┌───────────── domovoy (Docker) ─────────────┐
                                                        │ internal/bot     команды, кнопки, доступ   │
                                                        │      │ SmartHome interface                 │
                                                        │ internal/aqara   подпись, токены, intents ─┼──► Aqara Open API
                                                        │      │                                     │    (open-*.aqara.com)
                                                        │ /data/aqara_token.json  (Docker-том)       │
                                                        └────────────────────────────────────────────┘
```

## Пакеты

| Пакет | Назначение |
|---|---|
| `cmd/domovoy` | Точка входа. Читает конфиг, собирает зависимости, разбирает CLI (`auth`, `version`), обрабатывает SIGTERM. |
| `internal/config` | Настройки из переменных окружения, их проверка. |
| `internal/aqara` | Клиент Aqara Open API v3. Ничего не знает о Telegram. |
| `internal/tuya` | Клиент Tuya Cloud Open API: подпись HMAC-SHA256, токен (simple mode), дома, устройства, спецификации DP. |
| `internal/home` | Интерфейс `Home` (устройства, показания) и адаптеры Aqara и Tuya. Команды `/devices`, `/status` работают через него. |
| `internal/bot` | Логика Telegram. От Aqara зависит только через интерфейс `SmartHome`, поэтому его можно тестировать с подменой. |

## Aqara Open API

Все вызовы идут через `POST {AQARA_API_URL}/v3.0/open/api` с телом `{"intent": "...", "data": {...}}`.

Заголовки: `Appid`, `Keyid`, `Nonce`, `Time` (мс), `Accesstoken`, `Sign`, где

```
Sign = md5( lower( "Accesstoken=…&Appid=…&Keyid=…&Nonce=…&Time=…" + AppKey ) )
```

Для `config.auth.*` часть `Accesstoken=…&` пропускается.

Используемые intents:

| Intent | Где |
|---|---|
| `config.auth.getAuthCode` / `getToken` / `refreshToken` | `auth.go` |
| `query.device.info` | `/devices` |
| `query.position.info`, `query.scene.listByPositionId` | `/scenes` |
| `config.scene.run` | кнопка сценария |
| `query.resource.value`, `query.resource.name`, `write.resource.device` | `/light`: состояние, имена каналов, переключение |

## Tuya Cloud API

Подпись: `sign = HMAC-SHA256(secret, client_id + access_token + t + nonce + stringToSign)`, где
`stringToSign = METHOD \n sha256(body) \n \n /path?отсортированный=query`. Токен (`grant_type=1`) живёт ~2 ч,
клиент берёт новый по истечении или при кодах 1010–1012.

| Endpoint | Для чего |
|---|---|
| `GET /v1.0/token?grant_type=1` | токен доступа |
| `GET /v1.0/homes/{home_id}` | название дома |
| `GET /v1.0/homes/{home_id}/devices` | устройства дома с текущими DP (`status`) |
| `GET /v1.0/iot-03/devices/{id}/specification` | единицы и `scale` DP (кэшируется) |

Значения целочисленных DP приходят без запятой: `value / 10^scale`. Коды, которые бот показывает в `/status`:
`va_temperature`/`temp_current`, `va_humidity`/`humidity_value`, `battery_percentage`, `power_total`/`cur_power`
(приводится к Вт), `forward_energy_total`/`add_ele` (кВт·ч).

## Несколько домов

У каждого дома ID (`aqara`, `tuya-<home_id>`) и название. Чат может иметь доступ к нескольким домам;
тогда `/devices` и `/status` сначала показывают кнопки выбора дома (`home:<cmd>:<homeID>`).
Пульт `/light`, сценарии и алерты о протечке пока работают только для дома Aqara.

## Доступ

Проверка идёт по **ID чата**, а не пользователя. Любой участник семейного чата может управлять домом, а из чужих чатов бот откажет. Исключение — `/chatid`, он нужен для настройки.

Разрешённые чаты складываются из трёх источников: `TELEGRAM_ALLOWED_CHAT_IDS`, сам `TELEGRAM_ADMIN_CHAT_ID`
и чаты, одобренные администратором (`internal/bot/access.go`, файл `DATA_DIR/access.json`, запись через tmp + rename).

```
незнакомый чат ──/start──▶ бот ──🔔 запрос [✅|❌]──▶ личка админа
        ▲                                            │ callback acc:ok:<chatID>:<homeID|*>, acc:no:<chatID>
        └──────────── «доступ открыт / отклонён» ◀───┘ (проверка: нажато в чате админа)
```

Ожидающие запросы хранятся в памяти и не дублируются. Отказы сохраняются, чтобы один и тот же чат не засыпал админа запросами.

## Решения

- **Long polling, а не webhook:** не нужны домен, TLS и открытые порты на VPS.
- **Токен в файле на томе:** это единственное состояние бота. Отдельная БД не нужна.
- **Минимум зависимостей:** HTTP-клиент Aqara написан на `net/http`, логи пишутся через `log/slog`.

## Пульт `/light`

Поддерживаемые модели и их ресурсы on/off заданы в `switchChannels` в файле `internal/bot/light.go`:

| Модель | Ресурсы | Значения |
|---|---|---|
| `lumi.relay.c2acn01` (реле, 2 канала) | `4.1.85`, `4.2.85` | `0` — выкл, `1` — вкл |
| `lumi.plug.maeu01` (розетка EU) | `4.1.85` | `0` / `1` |

Чтобы добавить модель, посмотрите её ресурсы через `query.resource.info` (`{"model": "..."}`) и допишите их в `switchChannels`.

При нажатии кнопки бот сначала читает текущее состояние канала, потом записывает противоположное и перерисовывает панель прямо в том же сообщении (`editMessageText`), а не присылает новое. Подписи кнопок берутся из названия устройства и названия канала. Автоматические названия вроде «переключатель 1» заменяются номером канала.

## Алерты

```
 датчик ─► шлюз M2 ─► облако Aqara ──HTTP push──► Caddy :443 ─► bot :8080 /aqara/push/<secret>
                                                                         │
                                         резервный опрос (5 мин) ──► alert.Watcher ──► Telegram
```

- `aqara.PushHandler` принимает сообщения `resource_report` и **на любой запрос отвечает `{"code":0}`**, в том числе на проверку «Verify» из консоли. Если доля ошибок превысит 5% за 5 минут, Aqara приостановит push.
- `alert.Watcher` запоминает последнее значение каждого отслеживаемого ресурса. Если одно и то же изменение пришло и через push, и через опрос, уведомление уходит один раз.
- При старте (и при появлении новых датчиков) бот вызывает `config.resource.subscribe` для отслеживаемых ресурсов: в режиме консоли **User-defined subscription** push без подписки не приходит. В режиме «All receiving» Aqara отвечает отказом, это нормально.
- Если протечка уже активна при старте бота, алерт тоже отправляется.
- Аутентификация push — секретный сегмент пути (`PUSH_SECRET`). Заголовок `token`, то есть access token аккаунта, сверяется с текущим токеном, но при расхождении сообщение не отбрасывается: гонка при обновлении токена не должна стоить пропущенной протечки.

Правила (`alert.DefaultRules`):

| Ресурс | Модели | Значения |
|---|---|---|
| `3.1.85` — протечка | `lumi.flood.agl02` и др. датчики протечки | `1` — протечка, `0` — сухо (тоже уведомление) |
| `8.0.9001` — низкий заряд | любые | `1` — низкий |
