FROM golang:1.24-alpine AS build

WORKDIR /src
ARG GOOSE_VERSION=v3.26.0

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN go install github.com/pressly/goose/v3/cmd/goose@${GOOSE_VERSION}
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/api ./cmd/api

FROM alpine:3.20

RUN apk add --no-cache ffmpeg
RUN adduser -D -H -s /sbin/nologin app
USER app

COPY --from=build /bin/api /bin/api
COPY --from=build /go/bin/goose /bin/goose
COPY --from=build /src/migrations /migrations

EXPOSE 8080

ENTRYPOINT ["/bin/api"]
