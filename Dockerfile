FROM golang:1.24-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN go install github.com/pressly/goose/v3/cmd/goose@latest
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/api ./cmd/api

FROM alpine:3.20

RUN adduser -D -H -s /sbin/nologin app
USER app

COPY --from=build /bin/api /bin/api
COPY --from=build /go/bin/goose /bin/goose
COPY --from=build /src/migrations /migrations

EXPOSE 8080

ENTRYPOINT ["/bin/api"]
