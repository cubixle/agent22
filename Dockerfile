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

ARG OPENCODE_VERSION=1.2.10
ARG OPENCODE_LINUX_X64_SHA256=ebcc24012e8f067b10d7416430c88e9c429115ecfbccf8da9eb59db3b629a358
ARG OPENCODE_LINUX_ARM64_SHA256=d9a9d4f0bc1ed246258c0e8846e80593755a72bf4afd3940c4071d6f0b7b7775

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        git \
        openssh-client \
    && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
        amd64) opencode_arch="x64"; opencode_sha256="$OPENCODE_LINUX_X64_SHA256" ;; \
        arm64) opencode_arch="arm64"; opencode_sha256="$OPENCODE_LINUX_ARM64_SHA256" ;; \
        *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac; \
    curl -fsSLo /tmp/opencode.tar.gz "https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/opencode-linux-${opencode_arch}.tar.gz"; \
    echo "${opencode_sha256}  /tmp/opencode.tar.gz" | sha256sum -c -; \
    tar -xzf /tmp/opencode.tar.gz -C /usr/local/bin opencode; \
    chmod 0755 /usr/local/bin/opencode; \
    rm -f /tmp/opencode.tar.gz

RUN useradd --create-home --shell /bin/bash agent22 \
    && mkdir -p /home/agent22/.local/share/opencode /home/agent22/.opencode /home/agent22/.ssh /workspace \
    && ln -sf /home/agent22/.local/share/opencode/auth.json /home/agent22/.opencode/auth.json \
    && chmod 700 /home/agent22/.ssh \
    && chown -R agent22:agent22 /home/agent22 /workspace

WORKDIR /workspace

COPY --from=builder /out/agent22 /usr/local/bin/agent22

USER agent22

CMD ["agent22"]
