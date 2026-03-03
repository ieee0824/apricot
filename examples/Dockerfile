FROM golang:1.25 AS builder
WORKDIR /src
COPY main.go .
COPY go.mod .
RUN go build -o app .

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=builder /src/app .
EXPOSE 8080
CMD ["/app/app"]
