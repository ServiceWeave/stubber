FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY main.go .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o server main.go

FROM scratch
COPY --from=builder /app/server /server
EXPOSE 8080
CMD ["/server"]
