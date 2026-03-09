.PHONY: up down ps logs migrate api worker collector frontend backend-build frontend-build check

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

backend-build:
	cd backend && go build ./...

frontend-build:
	cd frontend && npm install && npm run build

check: backend-build frontend-build
