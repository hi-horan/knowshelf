# syntax=docker/dockerfile:1.12

FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build

WORKDIR /src

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    goarm64="${TARGETVARIANT#v}"; \
    if [ "$goarm64" = "$TARGETVARIANT" ]; then goarm64=""; fi; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM64=$goarm64 \
	    go build \
	        -trimpath \
	        -ldflags="-s -w -X main.gitVersion=${COMMIT} -X main.buildTime=${BUILD_DATE} -X main.gitTag=${VERSION}" \
	        -o /out/knowshelf \
	        ./cmd/knowshelf; \
	    mkdir -p /out/data; \
	    : > /out/data/.keep

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/knowshelf /usr/local/bin/knowshelf
COPY --from=build --chown=nonroot:nonroot /out/data /app/data
COPY config.example.yaml /etc/knowshelf/config.yaml

EXPOSE 8765
VOLUME ["/app/data"]

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/knowshelf"]
CMD ["run", "-c", "/etc/knowshelf/config.yaml"]
