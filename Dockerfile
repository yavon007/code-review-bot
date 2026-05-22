FROM golang:1.26-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /out/code-review-bot .

FROM alpine:3.22

RUN adduser -D -H appuser
WORKDIR /app
COPY --from=build /out/code-review-bot /app/code-review-bot

USER appuser
EXPOSE 8080

ENTRYPOINT ["/app/code-review-bot"]
