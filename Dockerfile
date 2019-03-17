FROM golang:1.12 as builder

ENV GO111MODULE=on
WORKDIR /go/src/github.com/icco/graphql
COPY . .

RUN cd tool && go build -o /go/bin/tool .
RUN /go/bin/tool -action=validate
RUN /go/bin/tool -action=generate

FROM halverneus/static-file-server:latest
COPY --from=builder /go/src/github.com/icco/graphql/output/ /web/
