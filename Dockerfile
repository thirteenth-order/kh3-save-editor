# syntax=docker/dockerfile:1

# kh3save in a container: a static binary on an empty filesystem.
#
# The runtime stage is FROM scratch, so the image contains the binary, two
# empty directories and a passwd entry -- no shell, no libc, no package
# manager, nothing to escalate into. That is the whole hardening argument;
# the run-time flags in compose.yaml and the README only take away what is
# left.
#
# The build downloads nothing: this project has no third-party dependencies,
# so `docker build --network none` works once the builder image is pulled.

ARG GO_VERSION=1.24

FROM golang:${GO_VERSION}-alpine AS build

# Set by buildx; empty under a plain `docker build`, which is what we want,
# because empty means "build for whatever this machine is".
ARG TARGETOS
ARG TARGETARCH
# Stamped into `kh3save version`. The Makefile passes git describe; a bare
# `docker build` gets "docker" so the binary never claims to be a release.
ARG VERSION=docker

WORKDIR /src

# go.mod alone, then the sources: this layer is the whole dependency graph,
# and it is empty, so it caches forever.
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
      -trimpath -buildvcs=false \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/kh3save ./cmd/kh3save \
 && /out/kh3save version

# Everything the scratch image needs besides the binary, assembled here
# because scratch has no shell to assemble it in.
#
#   /saves   the mount point for a save folder or a backup .zip
#   /config  where the UI remembers folders you pointed it at; a tmpfs at
#            run time, so it exists only for as long as the container does
#
# 65532 is the conventional "nonroot" uid. It is a default, not a
# requirement: pass --user "$(id -u):$(id -g)" so files written into a
# bind-mounted save folder belong to you and not to a uid you have never
# heard of.
# The UI finds save folders by globbing the places the game puts them, all of
# them under $HOME. A container has no such folder, so $HOME/Documents holds a
# link to the mount point and the existing autodetection walks into it -- which
# is why the UI lists a save the moment it opens, with nothing typed. The name
# has to be exactly this: it is the folder name the glob looks for. Mounting
# something that is not a save folder at /saves just leaves the link dangling
# and the UI shows its usual "paste a path" field.
RUN mkdir -p /rootfs/saves /rootfs/config /rootfs/home/nonroot/Documents \
 && ln -s /saves "/rootfs/home/nonroot/Documents/KINGDOM HEARTS III" \
 && chown -R 65532:65532 /rootfs \
 && echo 'nonroot:x:65532:65532:nonroot:/home/nonroot:/sbin/nologin' > /rootfs/etc-passwd \
 && echo 'nonroot:x:65532:' > /rootfs/etc-group

FROM scratch

COPY --from=build /rootfs/etc-passwd /etc/passwd
COPY --from=build /rootfs/etc-group /etc/group
COPY --from=build --chown=65532:65532 /rootfs/saves /saves
COPY --from=build --chown=65532:65532 /rootfs/config /config
COPY --from=build --chown=65532:65532 /rootfs/home/nonroot /home/nonroot
COPY --from=build /out/kh3save /usr/local/bin/kh3save

# HOME is what the save-folder autodetection searches, and XDG_CONFIG_HOME is
# where the UI's folder list goes. Both point at directories that survive a
# read-only root filesystem, so nothing has to be writable but the mount.
ENV HOME=/home/nonroot \
    XDG_CONFIG_HOME=/config \
    KH3_ADDR=0.0.0.0:8787

# Gate 1 of the UI's security model is a loopback bind, and a loopback bind
# inside a network namespace is reachable from nothing at all -- so the image
# opts out with KH3_ADDR above, and the container boundary takes over. Publish
# this port to 127.0.0.1 and no further; the per-run token, the Host check and
# the Sec-Fetch-Site check all still apply.
EXPOSE 8787

USER 65532:65532
WORKDIR /saves

ENTRYPOINT ["/usr/local/bin/kh3save"]
# No arguments would open the UI and try to launch a browser that is not in
# here. Say what we mean instead.
CMD ["gui", "-no-browser"]

LABEL org.opencontainers.image.title="kh3save" \
      org.opencontainers.image.description="Offline save tools for Kingdom Hearts III (PC)" \
      org.opencontainers.image.source="https://github.com/thirteenth-order/kh3-save-editor" \
      org.opencontainers.image.licenses="GPL-3.0-or-later"
