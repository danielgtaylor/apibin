FROM golang:1.18-alpine3.15 as build

WORKDIR /build
COPY go.mod go.sum ./
RUN apk update && apk add --no-cache bash git && go mod download
COPY . .
RUN go install

FROM alpine:3.15
COPY --from=build /go/bin/apibin /usr/local/bin/
ENTRYPOINT [ "apibin" ]
EXPOSE 8888
