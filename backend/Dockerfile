# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS backend-builder
ARG TARGETARCH
WORKDIR /app/backend

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /out/backend-server ./cmd/main.go

FROM debian:bookworm-slim AS backend
WORKDIR /app/backend

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*

COPY --from=backend-builder /out/backend-server /usr/local/bin/backend-server
RUN mkdir -p /app/backend/data

EXPOSE 8080
CMD ["backend-server"]
