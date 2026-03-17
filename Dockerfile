# Stage 1: Build
FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=1.0.0" -o /leavesafe ./cmd/leavesafe

# Stage 2: Runtime (scratch = ~7MB total image)
FROM scratch
COPY --from=builder /leavesafe /leavesafe
EXPOSE 8080
ENTRYPOINT ["/leavesafe"]
