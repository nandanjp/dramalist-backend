FROM golang:1.25-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /auth-service .

FROM alpine:3.20
# ca-certificates is required for HTTPS calls to OAuth providers
RUN apk add --no-cache ca-certificates

# /keys is a named Docker volume shared read-only with the gateway.
# auth-service writes the RSA key pair here on first boot.
RUN mkdir -p /keys

COPY --from=builder /auth-service /auth-service

ENV GIN_MODE=release
ENV PORT=3001

EXPOSE 3001
CMD ["/auth-service"]
