FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o build/matey cmd/matey/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates curl && \
    curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
    chmod +x kubectl && \
    mv kubectl /usr/local/bin/
WORKDIR /app

COPY --from=builder /app/build/matey .

ENTRYPOINT ["./matey"]