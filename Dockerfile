FROM golang:1.12
ENV GO111MODULE=on
WORKDIR /go/src/github.com/icco/graphql
COPY . .
RUN cd tool && go build -o /go/bin/tool .
RUN /go/bin/tool -action=validate
RUN /go/bin/tool -action=generate
