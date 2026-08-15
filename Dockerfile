FROM golang:1.26.6-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/monitor ./cmd/monitor

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 monitor
COPY --from=build /out/monitor /usr/local/bin/monitor
USER monitor
ENTRYPOINT ["/usr/local/bin/monitor"]
