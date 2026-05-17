FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -o /out/sentinel-controller ./controller/cmd/sentinel-controller

FROM alpine:3.20

COPY --from=build /out/sentinel-controller /usr/local/bin/sentinel-controller
ENTRYPOINT ["sentinel-controller"]
