.PHONY: up down ps logs migrate api worker collector frontend youtube-service backend-build frontend-build check clean-runtime reset-db reset-local-state prod-build prod-up prod-down prod-logs

up:
	docker compose up -d

down:
	docker compose down

ps:
	docker compose ps

logs:
	docker compose logs -f --tail=200

migrate:
	cd backend && go run ./cmd/migrate

api:
	cd backend && go run ./cmd/api

worker:
	cd backend && go run ./cmd/worker

collector:
	cd backend && go run ./cmd/collector

frontend:
	cd frontend && npm install && npm run dev

youtube-service:
	cd services/youtube_audio_service && uvicorn app:app --host 0.0.0.0 --port 8090

backend-build:
	cd backend && go build ./...

frontend-build:
	cd frontend && npm install && npm run build

check: backend-build frontend-build

clean-runtime:
	find backend/data -mindepth 1 -maxdepth 1 ! -name 'tg.session.json' -exec rm -rf {} +
	rm -rf frontend/dist/*
	rm -f data/*.log
	find . -name '.DS_Store' -delete

reset-db:
	docker compose up -d postgres
	docker compose exec -T postgres psql -U $${POSTGRES_USER:-postgres} -d $${POSTGRES_DB:-tgrag} -v ON_ERROR_STOP=1 < deploy/reset_app_tables.sql

reset-local-state: clean-runtime
	docker compose down -v --remove-orphans

prod-build:
	docker compose -f deploy/docker-compose.prod.yml --env-file .env build

prod-up:
	docker compose -f deploy/docker-compose.prod.yml --env-file .env up -d

prod-down:
	docker compose -f deploy/docker-compose.prod.yml --env-file .env down

prod-logs:
	docker compose -f deploy/docker-compose.prod.yml --env-file .env logs -f --tail=200
