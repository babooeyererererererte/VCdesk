FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
# Generate the module checksums inside the image before compiling.
RUN go mod tidy
RUN mkdir -p /out
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/vcdesk-signaling .

FROM alpine:3.21
RUN adduser -D -H -u 10001 app
COPY --from=build /out/vcdesk-signaling /usr/local/bin/vcdesk-signaling
USER app
ENV PORT=10000
EXPOSE 10000
ENTRYPOINT ["/usr/local/bin/vcdesk-signaling"]
