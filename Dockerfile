FROM golang:1-alpine AS builder

RUN apk add --no-cache ca-certificates git
COPY . /build
WORKDIR /build
RUN CGO_ENABLED=0 go build -o /usr/bin/yeetserv

FROM scratch

ENV LISTEN_ADDRESS=:8080
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/bin/yeetserv /usr/bin/yeetserv

CMD ["/usr/bin/yeetserv"]
