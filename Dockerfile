# stillroomd — org-wide search over your team's distilled knowledge.
#
# The image contains exactly one static binary. No database, no asset volume,
# no network egress needed to render a page: the UI is embedded in the binary
# and every document is derived from .team-context/ directories you mount in.
#
#   docker build -t stillroomd .
#   docker run --rm -p 8080:8080 -v ~/code:/checkouts:ro stillroomd -scan /checkouts
#
# Mount read-only. The server never writes to a repo — knowledge changes ride
# pull requests, not this service.

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/stillroomd ./cmd/stillroomd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/stillroomd /stillroomd
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/stillroomd"]
CMD ["-scan", "/checkouts"]
