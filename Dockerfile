FROM node:22-bookworm-slim AS operations
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --legacy-peer-deps --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM node:22-bookworm-slim AS dashboard
WORKDIR /dashboard
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY dashboard/ ./
# Type-only imports keep the durable Go control-plane contract shared.
COPY web/src/controlplane/api.ts /web/src/controlplane/api.ts
RUN npm run build

FROM golang:1.26-bookworm AS builder

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends build-essential git && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=1 GOOS=linux go build -buildvcs=false -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.BuildDate=${BUILD_DATE}'" -o ./CLIProxyAPI ./cmd/server/

FROM debian:bookworm

RUN apt-get update && apt-get install -y --no-install-recommends tzdata ca-certificates curl && rm -rf /var/lib/apt/lists/*

RUN mkdir /CLIProxyAPI

COPY --from=builder ./app/CLIProxyAPI /CLIProxyAPI/CLIProxyAPI

COPY config.example.yaml /CLIProxyAPI/config.example.yaml
COPY --from=dashboard /dashboard/dist/index.html /CLIProxyAPI/dashboard/dist/index.html
COPY dashboard/LICENSE.upstream dashboard/UPSTREAM.json /CLIProxyAPI/dashboard/
COPY --from=operations /web/dist/index.html /CLIProxyAPI/web/dist/index.html
COPY web/LICENSE.CPA-Manager-Plus web/NOTICE.md /CLIProxyAPI/web/
RUN mkdir -p /data && chmod 700 /data

WORKDIR /CLIProxyAPI

EXPOSE 8317
VOLUME ["/data"]
HEALTHCHECK --interval=30s --start-period=10s --timeout=5s --retries=3 CMD curl --fail --silent http://127.0.0.1:8317/healthz >/dev/null || exit 1

ENV TZ=Asia/Shanghai

RUN cp /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone

CMD ["./CLIProxyAPI"]
