# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/agent22 .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/agent22 /usr/local/bin/agent22

CMD ["agent22"]
