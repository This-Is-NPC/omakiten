FROM alpine:3.22
RUN apk add --no-cache git jq ca-certificates
COPY okt okt-server /usr/local/bin/
RUN mkdir -p /work
WORKDIR /work
