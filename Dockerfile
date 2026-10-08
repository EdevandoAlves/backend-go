FROM golang:1.27.1 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/server ./cmd/server

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app
COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /src/migrations /migrations
WORKDIR /
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
