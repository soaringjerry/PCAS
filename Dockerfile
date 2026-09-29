FROM golang:1.26.8-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/pcas ./cmd/pcas

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 pcas
COPY --from=build /out/pcas /usr/local/bin/pcas
USER pcas
ENTRYPOINT ["pcas"]
CMD ["serve"]
