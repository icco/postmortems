FROM golang:1.26-alpine as builder

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

# Drop to a non-root user for the runtime serve process.
# The validate/generate steps above run as root during the build and
# create world-readable output that the app user can serve without
# needing write access.
RUN adduser -S -u 1001 app
USER app

CMD /go/bin/pm -action=serve
