# syntax=docker/dockerfile:1

# Image chạy server yuumi (issue #49). Runtime cần đủ bộ công cụ mà 1 review
# job dùng: git (clone PR), Go toolchain (gofmt/go vet trên repo Go) và
# Claude CLI — để sau này chính image này chạy được sandbox riêng cho mỗi
# job (issue #78 giai đoạn 2).

ARG GO_VERSION=1.26.5

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/yuumi-server ./cmd/server

# Runtime dùng luôn image golang (đã có go + git + curl): go vet cần toolchain
# thật, tự ráp Go vào image slim không tiết kiệm được bao nhiêu.
FROM golang:${GO_VERSION}-bookworm

# Ghim đúng bản đã kiểm chứng các flag bảo mật (--setting-sources,
# --strict-mcp-config, chặn đọc ngoài thư mục review — xem PR #85). Nâng
# phiên bản thì kiểm chứng lại các flag đó trước, và cập nhật checksum từ
# https://downloads.claude.ai/claude-code-releases/<version>/manifest.json
# (platforms.linux-x64 / linux-arm64).
ARG CLAUDE_VERSION=2.1.281
ARG CLAUDE_SHA256_AMD64=56fe3da88458465fb27d7e9299dddb3fead55750fb9c2de795f233b5eea6dce1
ARG CLAUDE_SHA256_ARM64=dd27b36438a4fed1670cd29bad2fda6a73b628b6da55443e5c2f647fe6ed328f
ARG TARGETARCH

# DISABLE_AUTOUPDATER: Claude CLI native tự cập nhật, trong container phải
# giữ đúng bản đã ghim. Set trước bước cài để cả lệnh `claude --version`
# kiểm bản ghim bên dưới cũng không tự cập nhật.
ENV DISABLE_AUTOUPDATER=1

# Tải thẳng binary đúng bản + kiểm sha256 ghim sẵn, thay vì curl install.sh |
# bash (script lấy từ mạng mỗi lần build, và tự tải bản latest trước khi cài
# bản ghim). Cài vào /usr/local/bin bằng root: user yuumi (và code PR) không
# ghi đè được binary.
RUN set -eu; \
    case "${TARGETARCH:-amd64}" in \
      amd64) platform=linux-x64;   sha="${CLAUDE_SHA256_AMD64}" ;; \
      arm64) platform=linux-arm64; sha="${CLAUDE_SHA256_ARM64}" ;; \
      *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /usr/local/bin/claude \
      "https://downloads.claude.ai/claude-code-releases/${CLAUDE_VERSION}/${platform}/claude"; \
    echo "${sha}  /usr/local/bin/claude" | sha256sum -c -; \
    chmod 0755 /usr/local/bin/claude; \
    installed="$(claude --version | cut -d' ' -f1)"; \
    [ "${installed}" = "${CLAUDE_VERSION}" ] || { echo "claude version ${installed} != ${CLAUDE_VERSION}" >&2; exit 1; }

# Docker CLI để server tạo container sandbox cho mỗi job review (SANDBOX=docker,
# issue #78) qua docker.sock mount từ host. Chỉ lấy binary `docker` (client),
# không có daemon. Docker không công bố checksum cho bản static: sha256 dưới
# đây tính lúc ghim bản 29.8.1, nâng phiên bản thì tải về và tính lại.
ARG DOCKER_CLI_VERSION=29.8.1
ARG DOCKER_CLI_SHA256_AMD64=d8db66739d2e28d4933786d73e918d9be643a67fbd835db1bf740d650a259e70
ARG DOCKER_CLI_SHA256_ARM64=667395fbffab52901b80181dfbb39ea76da2fbd7642c4fbddd24e42146b07b48
RUN set -eu; \
    case "${TARGETARCH:-amd64}" in \
      amd64) arch=x86_64;  sha="${DOCKER_CLI_SHA256_AMD64}" ;; \
      arm64) arch=aarch64; sha="${DOCKER_CLI_SHA256_ARM64}" ;; \
      *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/docker.tgz \
      "https://download.docker.com/linux/static/stable/${arch}/docker-${DOCKER_CLI_VERSION}.tgz"; \
    echo "${sha}  /tmp/docker.tgz" | sha256sum -c -; \
    tar -xzf /tmp/docker.tgz -C /usr/local/bin --strip-components=1 docker/docker; \
    rm /tmp/docker.tgz; \
    docker --version

# Không chạy bằng root: code PR không tin cậy được clone và chạy go vet
# trong container này.
RUN useradd --create-home --uid 10001 yuumi \
    && mkdir -p /app/logs \
    && chown -R yuumi:yuumi /app

USER yuumi
WORKDIR /app

COPY --from=build /out/yuumi-server /usr/local/bin/yuumi-server

# Review log, review state, bundle cache đều ghi dưới /app/logs (đường dẫn
# mặc định tương đối "logs/..."): mount volume để không mất khi container
# bị tạo lại.
VOLUME /app/logs

EXPOSE 8080
# /health trả 503 khi Claude CLI hoặc GitHub App auth không dùng được, theo
# lần check gần nhất (server check lại mỗi 5 phút, xem cmd/server/main.go).
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s \
    CMD curl -fsS http://localhost:8080/health || exit 1

ENTRYPOINT ["yuumi-server"]
