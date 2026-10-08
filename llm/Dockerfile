FROM golang:1.27.1-trixie AS build

WORKDIR /src/llm

COPY shared/ /src/shared/
COPY llm/go.mod llm/go.sum ./
RUN go mod download

# Build
COPY llm/ ./

# pure go, so a static binary with no cgo
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app .


# Deploy
FROM gcr.io/distroless/static-debian13
COPY --from=build /out/app /bin/app
CMD ["/bin/app"]
