FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/seatbook ./cmd/api

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -S seatbook && adduser -S -G seatbook seatbook
WORKDIR /app
COPY --from=build /out/seatbook /app/seatbook
USER seatbook
EXPOSE 8080
ENTRYPOINT ["/app/seatbook"]
