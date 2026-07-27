FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -trimpath -o /out/attesta-controller ./controller/cmd/attesta-controller

FROM alpine:3.20

COPY --from=build /out/attesta-controller /usr/local/bin/attesta-controller
ENTRYPOINT ["attesta-controller"]
