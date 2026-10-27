FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /api ./cmd && CGO_ENABLED=0 go build -o /worker ./cmd/worker

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /api /api
COPY --from=build /worker /worker
EXPOSE 8080
CMD ["/api"]
