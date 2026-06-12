FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /stub-server .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /stub-server /stub-server
ENV PORT=8089
EXPOSE 8089
CMD ["/stub-server"]
