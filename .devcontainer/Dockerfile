FROM mcr.microsoft.com/devcontainers/go:2-1.25

USER root

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl gh \
    && case "$(dpkg --print-architecture)" in \
        amd64) arch=x64 ;; \
        arm64) arch=arm64 ;; \
        *) exit 1 ;; \
    esac \
    && curl -fL "https://github.com/github/copilot-cli/releases/latest/download/copilot-linux-${arch}.tar.gz" -o /tmp/copilot.tar.gz \
    && tar -xzf /tmp/copilot.tar.gz -C /usr/local/bin copilot \
    && rm -f /tmp/copilot.tar.gz \
    && rm -rf /var/lib/apt/lists/*

USER vscode
