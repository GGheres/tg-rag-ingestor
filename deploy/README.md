# Deploy

Этот каталог теперь содержит production-файлы для выкладки на сервер.

## Файлы

- `docker-compose.prod.yml` — production compose без bind-mount исходников.
- `reset_app_tables.sql` — очистка прикладных таблиц Postgres без удаления схемы миграций.

## Быстрый запуск на сервере

```bash
cp .env.example .env
```

Заполните секреты и обязательно проверьте:

- `POSTGRES_PASSWORD`
- `CORS_ALLOWED_ORIGIN`
- `TG_BOT_TOKEN` при необходимости запуска бота
- `DEEPGRAM_API_KEY`
- `MISTRAL_API_KEY`
- `OPENAI_API_KEY`
- `HH_CLIENT_ID`, `HH_CLIENT_SECRET`, `HH_USER_AGENT`
- `HH_REDIRECT_URI=https://<домен>/api/hh/oauth/callback` и такой же callback URL в настройках приложения HH

Сборка и запуск:

```bash
docker compose -f deploy/docker-compose.prod.yml --env-file .env build
docker compose -f deploy/docker-compose.prod.yml --env-file .env up -d
```

Запуск с Telegram-ботом:

```bash
docker compose -f deploy/docker-compose.prod.yml --env-file .env --profile bot up -d
```

Проверка:

```bash
docker compose -f deploy/docker-compose.prod.yml --env-file .env ps
docker compose -f deploy/docker-compose.prod.yml --env-file .env logs -f --tail=200
curl http://127.0.0.1:${FRONTEND_BIND_PORT:-80}/health
curl http://127.0.0.1:${FRONTEND_BIND_PORT:-80}/api/hh/config
```

Для автоматического сохранения HH токенов production compose монтирует корневой `.env` внутрь `api` и `tg-bot` как `/srv/app/.env` и выставляет `APP_ENV_FILE=/srv/app/.env`.

## Очистка локальных данных

Полная очистка runtime-артефактов:

```bash
make clean-runtime
```

Очистка только прикладных таблиц Postgres:

```bash
make reset-db
```

Полный локальный сброс со всеми docker volumes:

```bash
make reset-local-state
```
