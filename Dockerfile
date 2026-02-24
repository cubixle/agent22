# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/agent22 .

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        git \
        openssh-client \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://opencode.ai/install | bash

RUN useradd --create-home --shell /bin/bash agent22 \
    && mkdir -p /home/agent22/.local/share/opencode /home/agent22/.opencode /home/agent22/.ssh /workspace \
    && ln -sf /home/agent22/.local/share/opencode/auth.json /home/agent22/.opencode/auth.json \
    && chmod 700 /home/agent22/.ssh \
    && chown -R agent22:agent22 /home/agent22 /workspace

WORKDIR /workspace

COPY --from=builder /out/agent22 /usr/local/bin/agent22

USER agent22

CMD ["agent22"]
