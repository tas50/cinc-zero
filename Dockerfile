# Build a static cinc-zero binary and ship it on distroless.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -o /cinc-zero ./cmd/cinc-zero

FROM gcr.io/distroless/static-debian12
COPY --from=build /cinc-zero /cinc-zero
EXPOSE 8889
ENTRYPOINT ["/cinc-zero", "--addr", "0.0.0.0:8889"]
