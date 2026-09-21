FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
LABEL maintainer="nekohasekai <contact-git@sekai.icu>"
COPY . /go/src/github.com/sagernet/sing-box
WORKDIR /go/src/github.com/sagernet/sing-box
ARG TARGETOS TARGETARCH
ARG GOPROXY=""
ARG SING_BOX_VERSION=""
ARG SING_BOX_COMMIT=""
ENV GOPROXY ${GOPROXY}
ENV CGO_ENABLED=0
ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH
RUN set -ex \
    && apk add git build-base \
    && if [ -n "${SING_BOX_COMMIT}" ]; then export COMMIT="${SING_BOX_COMMIT}"; else export COMMIT=$(git rev-parse --short HEAD); fi \
    && if [ -n "${SING_BOX_VERSION}" ]; then export VERSION="${SING_BOX_VERSION}"; else export VERSION=$(go run ./cmd/internal/read_tag); fi \
    && export SHORT_COMMIT=$(printf '%.8s' "${COMMIT}") \
    && case "${VERSION}" in *"${SHORT_COMMIT}"*) : ;; *) echo "SING_BOX_VERSION must contain the bound commit prefix" >&2; exit 1 ;; esac \
    && export TAGS=$(cat release/DEFAULT_BUILD_TAGS_OTHERS) \
    && export LDFLAGS_SHARED=$(cat release/LDFLAGS) \
    && go build -v -trimpath -tags "$TAGS" \
        -o /go/bin/sing-box \
        -ldflags "-X \"github.com/sagernet/sing-box/constant.Version=$VERSION\" $LDFLAGS_SHARED -s -w -buildid=" \
        ./cmd/sing-box
FROM --platform=$TARGETPLATFORM alpine AS dist
LABEL maintainer="nekohasekai <contact-git@sekai.icu>"
RUN set -ex \
    && apk add --no-cache --upgrade bash tzdata ca-certificates nftables
COPY --from=builder /go/bin/sing-box /usr/local/bin/sing-box
ENTRYPOINT ["sing-box"]
