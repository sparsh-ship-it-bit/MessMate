FROM golang:1.24-alpine AS build
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o messmate ./cmd/server

FROM alpine:3.21
WORKDIR /app
COPY --from=build /app/messmate .
EXPOSE 8080
CMD ["./messmate"]
