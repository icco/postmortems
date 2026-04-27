# Build stage
FROM golang:1.26-alpine AS builder

ENV GOPROXY="https://proxy.golang.org"
ENV CGO_ENABLED=0

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -ldflags="-s -w" -o /pm ./tool

# Validate + generate as part of the build so the runtime image only needs to serve.
RUN /pm -action=validate && /pm -action=generate

# Final stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata
RUN adduser -S -u 1001 app

WORKDIR /app
COPY --from=builder --chown=app /pm /app/pm
COPY --from=builder --chown=app /src/data /app/data
COPY --from=builder --chown=app /src/templates /app/templates
COPY --from=builder --chown=app /src/static /app/static
COPY --from=builder --chown=app /src/output /app/output

USER app

EXPOSE 8080

ENTRYPOINT ["/app/pm", "-action=serve"]
