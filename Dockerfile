FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=none
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.version=${VERSION} -X main.gitCommit=${GIT_COMMIT} -X main.buildTime=${BUILD_TIME} -s -w" \
    -o /bin/nexusgate ./cmd/nexusgate/

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -g '' nexusgate

COPY --from=builder /bin/nexusgate /usr/local/bin/nexusgate
COPY configs/nexusgate.yaml /etc/nexusgate/nexusgate.yaml

USER nexusgate

EXPOSE 8080 8443 9091

ENTRYPOINT ["nexusgate"]
CMD ["-config", "/etc/nexusgate/nexusgate.yaml"]
