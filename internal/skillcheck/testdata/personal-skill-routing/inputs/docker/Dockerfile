FROM golang:1.27.1
WORKDIR /app
COPY . .
RUN go mod download
RUN go build -o service ./cmd/service
EXPOSE 8080
CMD ["./service"]
