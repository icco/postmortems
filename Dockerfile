FROM golang:1.23-alpine as builder

ENV GOPROXY="https://proxy.golang.org"
ENV GO111MODULE="on"
ENV NAT_ENV="production"
RUN apk add --no-cache git

WORKDIR /go/src/github.com/icco/postmortems
COPY . .

RUN go build -o /go/bin/pm ./tool

EXPOSE 8080

RUN /go/bin/pm -action=validate
RUN /go/bin/pm -action=generate
CMD /go/bin/pm -action=serve
