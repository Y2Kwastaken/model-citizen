FROM mwader/static-ffmpeg:7.1@sha256:a8090df5f5608daef387e1b2e93b98aaacb4d92153ad904e7d715c725724fca4 AS ffmpeg

FROM golang:1.27.1-trixie
WORKDIR /app

COPY --from=ffmpeg /ffmpeg /ffprobe /usr/local/bin/

# libdave: Discord's DAVE (E2EE) implementation. Required for voice.
ARG LIBDAVE_VERSION=v1.1.0

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl unzip pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Fetch the prebuilt shared library and install it where pkg-config can see it.
RUN set -eux; \
    case "$(uname -m)" in \
        x86_64) arch="X64" ;; \
        aarch64|arm64) arch="ARM64" ;; \
        *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/libdave.zip \
        "https://github.com/discord/libdave/releases/download/${LIBDAVE_VERSION}/cpp/libdave-Linux-${arch}-boringssl.zip"; \
    unzip -j -o /tmp/libdave.zip "include/dave/dave.h" -d /usr/local/include; \
    unzip -j -o /tmp/libdave.zip "lib/libdave.so"      -d /usr/local/lib; \
    rm -f /tmp/libdave.zip; \
    mkdir -p /usr/local/lib/pkgconfig; \
    printf '%s\n' \
        'prefix=/usr/local' \
        'exec_prefix=${prefix}' \
        'libdir=${exec_prefix}/lib' \
        'includedir=${prefix}/include' \
        '' \
        'Name: dave' \
        'Description: Discord Audio & Video End-to-End Encryption (DAVE) Protocol' \
        "Version: ${LIBDAVE_VERSION#v}" \
        'Libs: -L${libdir} -ldave -Wl,-rpath,${libdir}' \
        'Cflags: -I${includedir}' \
        > /usr/local/lib/pkgconfig/dave.pc; \
    ldconfig

# Sample track for /play. data/ is otherwise ignored (it holds .env).
COPY data/wave data/wave
COPY go.mod go.sum ./

RUN go mod download
COPY . .

# We use CGO to build against binaries like libdave
RUN CGO_ENABLED=1 GOOS=linux go build -o /model-citizen

# Run
CMD ["/model-citizen"]
