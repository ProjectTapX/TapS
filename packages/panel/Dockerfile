# --- build web ---
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm config set registry https://registry.npmmirror.com && npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- build panel ---
FROM golang:1.22-alpine AS panel
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0
COPY go.work ./
COPY packages/shared/ ./packages/shared/
COPY packages/panel/ ./packages/panel/
COPY packages/daemon/ ./packages/daemon/
RUN cd packages/panel && go build -trimpath -ldflags='-s -w' -o /out/panel ./cmd/panel

# --- runtime ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=panel /out/panel /app/panel
COPY --from=web /web/dist /app/web
ENV TAPS_ADDR=:24444 TAPS_DATA_DIR=/data TAPS_WEB_DIR=/app/web
EXPOSE 24444
VOLUME ["/data"]
ENTRYPOINT ["/app/panel"]
