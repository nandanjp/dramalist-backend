FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /user-service .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /user-service /user-service
ENV GIN_MODE=release
ENV PORT=3002
EXPOSE 3002
CMD ["/user-service"]
