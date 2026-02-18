# Stage 1: Build
FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$(cat VERSION 2>/dev/null || echo docker)" \
    -o /deskmon-agent ./cmd/deskmon-agent

# Stage 2: Runtime (~15MB image)
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /deskmon-agent /usr/local/bin/deskmon-agent

EXPOSE 7654
ENTRYPOINT ["deskmon-agent"]
