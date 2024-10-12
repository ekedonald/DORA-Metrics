FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o dora-metrics .

# final stage
FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/dora-metrics .
EXPOSE 4040
CMD ["./dora-metrics"]
