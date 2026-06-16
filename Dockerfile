# Build a static cinc-zero binary and ship it on distroless.
FROM golang:1.26 AS build
WORKDIR /src
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE} -s -w" -o /cinc-zero ./cmd/cinc-zero

FROM gcr.io/distroless/static-debian12
COPY --from=build /cinc-zero /cinc-zero
EXPOSE 8889
ENTRYPOINT ["/cinc-zero"]
CMD ["--addr", "0.0.0.0:8889"]
