FROM golang:1-alpine AS builder

RUN apk add --no-cache ca-certificates
COPY . /build
WORKDIR /build
RUN CGO_ENABLED=0 go build -o /usr/bin/scheduleserv

FROM scratch

ENV LISTEN_ADDRESS=:8080
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/bin/scheduleserv /usr/bin/scheduleserv

CMD ["/usr/bin/scheduleserv"]
