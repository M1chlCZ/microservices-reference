FROM golang:1.27-alpine AS build

ARG SERVICE

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/${SERVICE}

FROM alpine:3.21

RUN adduser -D -u 10001 app
USER app

COPY --from=build /out/service /service

ENTRYPOINT ["/service"]
