FROM golang:alpine AS builder
WORKDIR /tmp/pharmacy-consul
ADD . .
RUN apk add make && GOPATH=$(pwd)/cache make all

FROM consul:1.14.4
RUN apk add bind-tools
COPY --from=0 /tmp/pharmacy-consul/build/pharmacy-consul /usr/bin/
ENTRYPOINT ["/bin/sh", "-c", "pharmacy-consul && docker-entrypoint.sh agent"]