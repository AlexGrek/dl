# ---- Stage 1: Build frontend ----
FROM node:22-alpine AS frontend
WORKDIR /app
COPY dl-frontend/package.json dl-frontend/package-lock.json ./dl-frontend/
RUN cd dl-frontend && npm ci --silent
COPY dl-frontend/ ./dl-frontend/
RUN cd dl-frontend && npm run build

# ---- Stage 2: Build backend (embeds compiled frontend) ----
FROM golang:1.24-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY src/ ./src/
# Replace placeholder static dir with the real built frontend
RUN rm -rf ./src/static
COPY --from=frontend /app/dl-frontend/dist ./src/static/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o dl ./src/

# ---- Stage 3: Runtime ----
FROM debian:bookworm-slim
WORKDIR /app
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*
COPY --from=backend /app/dl ./dl
EXPOSE 8080
ENTRYPOINT ["/app/dl", "-secrets", "/etc/dl/secrets.yaml"]
