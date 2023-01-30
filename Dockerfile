FROM golang:alpine AS builder
WORKDIR /tmp/pharmacy-consul
ADD . .
RUN GOPATH=$(pwd)/cache CGO_ENABLED=0 go build -a -installsuffix cgo -o build/

FROM consul:1.14.4
COPY --from=0 /tmp/pharmacy-consul/build/pharmacy-consul /usr/bin/
ENTRYPOINT ["/bin/sh", "-c", "pharmacy-consul && consul agent"]