FROM golang:1.24-bullseye AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o build/matey cmd/matey/main.go

FROM debian:bullseye-slim
RUN apt-get update && apt-get install -y ca-certificates curl && \
    curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
    chmod +x kubectl && \
    mv kubectl /usr/local/bin/ && \
    apt-get clean && rm -rf /var/lib/apt/lists/*
WORKDIR /app

COPY --from=builder /app/build/matey .

ENTRYPOINT ["./matey"]