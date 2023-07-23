FROM golang:1.20 as builder

WORKDIR /app

COPY go.* ./
COPY *.go ./
COPY pkg/ pkg/ 
COPY cmd/ cmd/ 
COPY test/ test/

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dsnexec

FROM alpine:3.18

RUN apk add -U --no-cache udns libevent openssl ca-certificates postgresql-client
RUN adduser -D -S -s /bin/sh dsnexec

COPY --from=builder /dsnexec /usr/bin/dsnexec 
RUN chmod +x /usr/bin/dsnexec
RUN mkdir -p /var/run/dsnexec
WORKDIR /var/run/dsnexec

USER dsnexec

ENTRYPOINT ["/usr/bin/dsnexec"]
