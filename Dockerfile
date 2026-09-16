FROM node:22-alpine AS frontend
WORKDIR /app/web
COPY web/package.json ./
RUN npm install
COPY web/ .
RUN npm run build

FROM golang:1.24-alpine AS backend
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod ./
COPY . .
RUN go mod tidy
COPY --from=frontend /app/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -o messmate ./cmd/server

FROM alpine:3.21
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=backend /app/messmate .
COPY --from=backend /app/migrations ./migrations
COPY --from=backend /app/web/dist ./web/dist
EXPOSE 8080
CMD ["./messmate"]
