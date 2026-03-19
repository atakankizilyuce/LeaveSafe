FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /leavesafe ./cmd/leavesafe

FROM scratch
COPY --from=builder /leavesafe /leavesafe
ENTRYPOINT ["/leavesafe"]
