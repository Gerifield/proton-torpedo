FROM golang:1.25 AS build

WORKDIR /go/src/app
COPY . .

RUN go mod download
RUN CGO_ENABLED=0 go build -o /go/bin/torpedo -ldflags '-w -s' cmd/torpedo/main.go


FROM qmcgaw/gluetun:v3 AS production
WORKDIR /
COPY --from=build /go/bin/torpedo torpedo
COPY --from=build /go/src/app/static static
ENTRYPOINT ["./torpedo"]
