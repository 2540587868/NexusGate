FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates \
    && go env -w GOPROXY=https://goproxy.cn,direct

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=none
ARG BUILD_TIME=unknown

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-X main.version=${VERSION} -X main.gitCommit=${GIT_COMMIT} -X main.buildTime=${BUILD_TIME} -s -w" \
    -o /bin/nexusgate ./cmd/nexusgate/

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl && \
    adduser -D -g '' nexusgate

COPY --from=builder /bin/nexusgate /usr/local/bin/nexusgate
COPY configs/nexusgate.yaml /etc/nexusgate/nexusgate.yaml

USER nexusgate

EXPOSE 8080 8443 9090

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["nexusgate"]
CMD ["-config", "/etc/nexusgate/nexusgate.yaml"]
