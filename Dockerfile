FROM golang:1.12
ENV GO111MODULE=on
WORKDIR /go/src/github.com/icco/graphql
COPY . .
RUN cd tool && \
  go run . -action=validate -dir=../data && \
  go run . -action=generate -dir=../data
