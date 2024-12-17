FROM golang:1.23.3 AS builder
WORKDIR /app

# SQLite deps
RUN apt-get update && apt-get install -y gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -o main ./cmd/server/main.go

# Final stage
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/go-tunol
COPY --from=builder /app/main .

# Create data directory for SQLite
RUN mkdir -p /opt/go-tunol/data && \
    chmod 755 /opt/go-tunol/data

EXPOSE 8001
CMD ["./main"]
