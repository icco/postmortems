FROM golang:1.13-alpine as builder

ENV GOPROXY="https://proxy.golang.org"
ENV GO111MODULE="on"
ENV NAT_ENV="production"
RUN apk add --no-cache git

WORKDIR /go/src/github.com/icco/postmortems
COPY . .

RUN cd tool && go build -o /go/bin/tool .
RUN /go/bin/tool -action=validate
RUN /go/bin/tool -action=generate

FROM halverneus/static-file-server:latest
COPY --from=builder /go/src/github.com/icco/postmortems/output/ /web/
COPY --from=builder /go/src/github.com/icco/postmortems/tmp/healthz /web/
