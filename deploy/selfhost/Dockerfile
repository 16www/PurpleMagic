FROM node:24-bookworm-slim AS console
WORKDIR /src/gm-console
COPY gm-console/package*.json ./
RUN npm ci --no-audit --fund=false
COPY gm-console/ ./
RUN npm run build

FROM golang:1.26.5-bookworm AS build
ENV CGO_ENABLED=0 GOWORK=off
WORKDIR /src/gateway
COPY gateway/go.mod gateway/go.sum ./
RUN go mod download
COPY gateway/ ./
RUN go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/PlayerGateway ./cmd/playergateway && \
    go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/GMService ./cmd/gmservice && \
    go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/GMIndex ./cmd/gmindex
WORKDIR /src/gm
COPY gm/go.mod gm/go.sum ./
RUN go mod download
COPY gm/ ./
RUN go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/GMTool ./cmd/gmtool

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ /opt/purplemagic/bin/
COPY --from=console /src/gm-console/dist/ /opt/purplemagic/web/
USER 10001:10001
WORKDIR /data
CMD ["/opt/purplemagic/bin/GMService", "-config", "/data/service.json"]
