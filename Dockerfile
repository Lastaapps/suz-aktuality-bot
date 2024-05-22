# syntax=docker/dockerfile:1

FROM golang:1.22.3-alpine

RUN mkdir /app
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN apk add build-base
RUN CGO_ENABLED=1 GOOS=linux go build -v -o /usr/local/bin/app ./

CMD ["app"]

