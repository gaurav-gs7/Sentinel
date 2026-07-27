FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -trimpath -o /out/attesta-api ./api/cmd/attesta-api

FROM alpine:3.20

WORKDIR /app
COPY --from=build /out/attesta-api /usr/local/bin/attesta-api
COPY templates /app/templates
EXPOSE 8080
ENTRYPOINT ["attesta-api"]
