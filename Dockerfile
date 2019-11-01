FROM golang:1.13.3-alpine as builder

ENV GOPROXY="https://proxy.golang.org"
ENV GO111MODULE="on"
ENV NAT_ENV="production"
RUN apk add --no-cache git

WORKDIR /go/src/github.com/icco/postmortems
COPY . .

RUN cd tool && go build -o /go/bin/pm .
RUN /go/bin/pm -action=validate
RUN /go/bin/pm -action=generate

FROM halverneus/static-file-server:latest
COPY --from=builder /go/src/github.com/icco/postmortems/output/ /web/
COPY --from=builder /go/src/github.com/icco/postmortems/tmp/healthz /web/
