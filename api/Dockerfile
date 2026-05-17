FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -o /out/sentinel-api ./api/cmd/sentinel-api

FROM alpine:3.20

WORKDIR /app
COPY --from=build /out/sentinel-api /usr/local/bin/sentinel-api
COPY templates /app/templates
EXPOSE 8080
ENTRYPOINT ["sentinel-api"]
