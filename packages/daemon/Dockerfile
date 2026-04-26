FROM golang:1.22-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0
COPY go.work ./
COPY packages/shared/ ./packages/shared/
COPY packages/daemon/ ./packages/daemon/
COPY packages/panel/ ./packages/panel/
RUN cd packages/daemon && go build -trimpath -ldflags='-s -w' -o /out/daemon ./cmd/daemon

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata docker-cli
WORKDIR /app
COPY --from=build /out/daemon /app/daemon
ENV TAPS_DAEMON_ADDR=:24445 TAPS_DAEMON_DATA=/data
EXPOSE 24445
VOLUME ["/data"]
ENTRYPOINT ["/app/daemon"]
