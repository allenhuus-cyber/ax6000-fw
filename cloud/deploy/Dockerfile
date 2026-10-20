FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY cloud ./cloud
RUN cd cloud && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/cloudsrv ./cmd/cloudsrv

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /out/cloudsrv /usr/local/bin/cloudsrv
EXPOSE 8443
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/cloudsrv", "-config", "/etc/ax6000-cloud/config.json"]
