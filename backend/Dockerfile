FROM golang:1.25-bookworm AS build

WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/tg-rag-api ./cmd/api \
    && CGO_ENABLED=0 GOOS=linux go build -o /out/tg-rag-bot ./cmd/bot \
    && CGO_ENABLED=0 GOOS=linux go build -o /out/tg-rag-migrate ./cmd/migrate

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        poppler-utils \
        tesseract-ocr \
        tesseract-ocr-eng \
        tesseract-ocr-rus \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /srv/app

COPY --from=build /out/tg-rag-api /usr/local/bin/tg-rag-api
COPY --from=build /out/tg-rag-bot /usr/local/bin/tg-rag-bot
COPY --from=build /out/tg-rag-migrate /usr/local/bin/tg-rag-migrate
COPY backend/migrations ./migrations

RUN mkdir -p /srv/app/data/exports /srv/app/data/filescan_extracts /srv/app/data/hh_extractions

CMD ["/usr/local/bin/tg-rag-api"]
