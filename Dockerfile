FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /strata-server ./cmd/server

# ── Runtime ───────────────────────────────────────────────────────

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /strata-server /usr/local/bin/strata-server
COPY migrations /migrations

EXPOSE 8080

ENTRYPOINT ["strata-server"]
