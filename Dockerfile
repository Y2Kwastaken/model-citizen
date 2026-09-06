FROM golang:1.27.1-trixie
WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download
COPY *.go ./

# BUILD
RUN CGO_ENABLED=0 GOOS=linux go build -o /model-citizen

# Run
CMD ["/model-citizen"]