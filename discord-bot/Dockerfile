FROM golang:1.27.1-trixie AS build

# Dependencies
RUN apt-get update && apt-get install -y --no-install-recommends unzip libopus-dev \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://raw.githubusercontent.com/disgoorg/godave/refs/heads/master/scripts/libdave_install.sh \
    -o /tmp/libdave_install.sh \
    && SHELL=/bin/sh sh /tmp/libdave_install.sh v1.1.0
ENV PKG_CONFIG_PATH=/root/.local/lib/pkgconfig

# onnxruntime for the wake word, must be the release onnxruntime_go in go.mod is built against
ARG ONNXRUNTIME_VERSION=1.29.1
RUN set -eux; \
    case "$(uname -m)" in \
        x86_64) arch="x64" ;; \
        aarch64|arm64) arch="aarch64" ;; \
        *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/onnxruntime.tgz \
        "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-${arch}-${ONNXRUNTIME_VERSION}.tgz"; \
    mkdir -p /out/onnxruntime; \
    tar -xzf /tmp/onnxruntime.tgz -C /tmp; \
    cp "/tmp/onnxruntime-linux-${arch}-${ONNXRUNTIME_VERSION}"/lib/libonnxruntime.so* /out/onnxruntime/; \
    rm -rf /tmp/onnxruntime.tgz /tmp/onnxruntime-linux-*

WORKDIR /src/discord-bot

COPY shared/ /src/shared/
COPY discord-bot/go.mod discord-bot/go.sum ./
RUN go mod download

# Build
COPY discord-bot/ ./

# build the binary
RUN CGO_ENABLED=1 go build -trimpath -tags nolibopusfile -ldflags="-s -w" -o /out/app .


# Deploy
FROM gcr.io/distroless/cc-debian13
COPY --from=build /root/.local/lib/libdave.so /usr/lib/libdave.so
# libopus for gopkg.in/hraban/opus.v2 (cgo); the glob matches the multiarch dir on amd64 and arm64
COPY --from=build /usr/lib/*-linux-gnu/libopus.so.0* /usr/lib/
# static ffmpeg/ffprobe: used by our playback pipe and by yt-dlp (go-ytdlp resolves them from PATH)
COPY --from=mwader/static-ffmpeg:9.0.2 /ffmpeg /ffprobe /usr/local/bin/
# onnxruntime and the wake word models, at the paths config.Default expects
COPY --from=build /out/onnxruntime/ /usr/local/lib/
COPY discord-bot/assets/wake/ /assets/wake/
# onnxruntime's telemetry shells out for a device id and crashes without /bin/sh
ENV ORT_DISABLE_TELEMETRY=1
COPY --from=build /out/app /bin/app
CMD ["/bin/app"]
