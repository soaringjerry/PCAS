FROM node:24.18.0-bookworm-slim AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web ./
RUN npm run build

FROM golang:1.26.8-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/pcas ./cmd/pcas

FROM node:24.18.0-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl tzdata poppler-utils tesseract-ocr tesseract-ocr-chi-sim ffmpeg \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g @openai/codex@0.159.0 \
    && npm cache clean --force \
    && groupadd -g 10001 pcas && useradd -u 10001 -g pcas -m pcas \
    && mkdir -p /var/lib/pcas/blobs /var/lib/pcas/codex /app/web \
    && chown -R pcas:pcas /var/lib/pcas
COPY --from=build /out/pcas /usr/local/bin/pcas
COPY --from=web /src/web/dist /app/web/dist
WORKDIR /app
USER pcas
ENTRYPOINT ["pcas"]
CMD ["serve"]
