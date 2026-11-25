FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /demo .

FROM alpine:3.21
RUN adduser -D -u 10001 demo
USER demo
COPY --from=build /demo /demo
EXPOSE 8080
ENTRYPOINT ["/demo"]
